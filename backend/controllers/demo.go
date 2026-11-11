// Package controllers expone la API REST de GESTOCK. Los controladores no
// contienen reglas de negocio: delegan en store, auth, IA, Telegram y
// notificaciones, y validan permisos en cada endpoint.
package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/store"
)

// DemoController conserva los endpoints de recepción existentes, ahora con
// autenticación, permisos, auditoría y eventos en tiempo real.
type DemoController struct {
	web.Controller
}

// Productos → GET /api/demo/productos
func (c *DemoController) Productos() {
	if !autorizado(&c.Controller, auth.PermInventarioLeer) {
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "productos": Almacen.Productos(), "resumen": Almacen.Resumen(),
	})
}

// Recepciones → GET /api/demo/recepciones
func (c *DemoController) Recepciones() {
	if !autorizado(&c.Controller, auth.PermRecepcionLeer) {
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "recepciones": Almacen.Recepciones(),
		"pendientes": Almacen.Pendientes(),
	})
}

// CrearRecepcion → POST /api/demo/recepciones
func (c *DemoController) CrearRecepcion() {
	if !autorizado(&c.Controller, auth.PermRecepcionEscribir) {
		return
	}
	u, _ := usuarioActual(&c.Controller)

	var sol store.SolicitudRecepcion
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &sol); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{
			"ok": false, "estado": store.EstadoError, "mensaje": "JSON inválido",
		})
		return
	}
	if sol.IDOperacion == "" {
		sol.IDOperacion = fmt.Sprintf("OP-%d", time.Now().UTC().UnixNano())
	}
	sol.Usuario = u.Nombre
	sol.Rol = string(u.Rol)

	rec, duplicado, err := Almacen.Recepcionar(sol)
	if err != nil {
		estado, status := store.EstadoDesconocido, http.StatusUnprocessableEntity
		if err == store.ErrCantidadInvalida || err == store.ErrCodigoVacio {
			estado, status = store.EstadoError, http.StatusBadRequest
		}
		responder(&c.Controller, status, map[string]any{
			"ok": false, "estado": estado, "codigo": sol.CodigoProducto, "mensaje": err.Error(),
		})
		return
	}

	// La orden se publica solo tras registrar el ingreso. Si MQTT falla, el
	// registro se conserva y queda pendiente de clasificación (reintentable).
	simulado := MQTT != nil && MQTT.Simulando()
	enviado := false
	if !duplicado && !simulado && MQTT != nil {
		if errPub := MQTT.PublicarClasificacion(rec); errPub != nil {
			Almacen.MarcarClasificacionPendiente(rec.IDOperacion, "Orden MQTT pendiente: "+errPub.Error())
			rec, _ = Almacen.RecepcionPorOperacion(rec.IDOperacion)
		} else {
			enviado = true
		}
	}

	if !duplicado {
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: "RECEPCION_REGISTRADA",
			Detalle: fmt.Sprintf("%d uds de %s (%s), operación %s", rec.Cantidad, rec.NombreProducto, rec.CodigoProducto, rec.IDOperacion),
			Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "MEDIA",
		})
		EventoRecepcion(rec, false, enviado, simulado)
	}

	producto, _ := Almacen.ProductoPorCodigo(rec.CodigoProducto)
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok":            true,
		"duplicado":     duplicado,
		"recepcion":     rec,
		"producto":      producto,
		"stock":         producto.Stock,
		"mqtt_enviado":  enviado,
		"simulado":      simulado,
		"clasificacion": rec.Clasificacion,
	})
}

// Reintentar → POST /api/demo/reintentar  {id_operacion}
func (c *DemoController) Reintentar() {
	if !autorizado(&c.Controller, auth.PermRecepcionEscribir) {
		return
	}
	u, _ := usuarioActual(&c.Controller)
	var cuerpo struct {
		IDOperacion string `json:"id_operacion"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil || cuerpo.IDOperacion == "" {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "Falta id_operacion"})
		return
	}
	rec, ok := Almacen.RecepcionPorOperacion(cuerpo.IDOperacion)
	if !ok {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "Operación no encontrada"})
		return
	}
	if MQTT == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "MQTT no inicializado"})
		return
	}
	if MQTT.Simulando() {
		Almacen.MarcarClasificacionPendiente(rec.IDOperacion, "Reintento en modo simulación")
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: "RECEPCION_REINTENTO", Detalle: "Reintento en modo simulación: " + rec.IDOperacion,
			Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "INFO",
		})
		emitirRecepcion(rec)
		responder(&c.Controller, http.StatusOK, map[string]any{
			"ok": true, "simulado": true,
			"mensaje": "Modo simulación: la clasificación se confirmará automáticamente por el ESP32 emulado.",
		})
		return
	}
	if err := MQTT.PublicarClasificacion(rec); err != nil {
		Almacen.MarcarClasificacionPendiente(rec.IDOperacion, "Orden MQTT pendiente: "+err.Error())
		responder(&c.Controller, http.StatusBadGateway, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "RECEPCION_REINTENTO", Detalle: "Orden MQTT reenviada: " + rec.IDOperacion,
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "INFO",
	})
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "mensaje": "Orden reenviada a la maqueta"})
}

// Sensor → POST /api/demo/sensor  {evento, estado, dispositivo}
// Permite simular una lectura del HC-SR04 sin hardware. No confirma ninguna
// clasificación: solo actualiza el estado del sensor.
func (c *DemoController) Sensor() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || (!u.TienePermiso(auth.PermIoTControlar) && !u.TienePermiso(auth.PermRecepcionEscribir)) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para simular el sensor"})
		return
	}
	var ev struct {
		Evento      string `json:"evento"`
		Estado      bool   `json:"estado"`
		Dispositivo string `json:"dispositivo"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &ev); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	nueva := Almacen.RegistrarDeteccion(ev.Dispositivo, ev.Estado)
	Almacen.SetESP32(true, time.Now().UTC().Format(time.RFC3339))
	EventoSensor(ev.Dispositivo, ev.Estado, nueva)
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "nueva": nueva, "sensor": Almacen.EstadoIoT().Sensor,
	})
}

// EstadoIoT → GET /api/demo/estado-iot
func (c *DemoController) EstadoIoT() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !puedeVerIoT(u) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para consultar IoT"})
		return
	}
	e := Almacen.EstadoIoT()
	if MQTT != nil {
		e.MQTTConectado = MQTT.Conectado()
		e.Simulacion = Cfg.Simulacion || MQTT.Simulando()
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "estado": e})
}

// Reiniciar → POST /api/demo/reiniciar (solo administrador)
func (c *DemoController) Reiniciar() {
	if !autorizado(&c.Controller, auth.PermSistemaAdmin) {
		return
	}
	u, _ := usuarioActual(&c.Controller)
	Almacen.Reiniciar()
	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "DEMO_REINICIADA", Detalle: "Catálogo, stock e historial restablecidos",
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "ALTA",
	})
	if Notificaciones != nil {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatAuditoria, Prioridad: notifications.PrioridadAlta,
			Titulo: "Datos de demostración reiniciados",
			Mensaje: "El administrador " + u.Nombre + " restableció el catálogo, el stock y el historial. Los movimientos anteriores ya no están disponibles.",
			Modulo: "/", Roles: []string{string(auth.RolAdmin), string(auth.RolAuditor)},
			Clave: "demo-reiniciada",
		})
	}
	emitirEstadoIoT()
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "mensaje": "Demostración reiniciada"})
}

// Health → GET /api/health (público, sin datos sensibles)
func (c *DemoController) Health() {
	responder(&c.Controller, http.StatusOK, map[string]any{
		"status":     "ok",
		"mqtt":       MQTT != nil && MQTT.Conectado(),
		"simulacion": Cfg.Simulacion || (MQTT != nil && MQTT.Simulando()),
		"ia":         IA != nil,
		"telegram":   Cfg.TelegramEnabled,
		"ws":         Hub != nil && Hub.Conexiones() > 0,
		"ts":         time.Now().UnixMilli(),
	})
}
