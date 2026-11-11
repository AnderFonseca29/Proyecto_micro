// Package controllers expone la API REST de la demostración.
package controllers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/broker"
	"gestock-iot-backend/config"
	"gestock-iot-backend/store"
)

// Dependencias inyectadas desde main.
var (
	Almacen *store.Store
	MQTT    *broker.Broker
	Cfg     config.Config
)

// DemoController agrupa los endpoints /api/demo/*.
type DemoController struct {
	web.Controller
}

// --- catálogo e historial ---

// Productos → GET /api/demo/productos
func (c *DemoController) Productos() {
	c.json(http.StatusOK, map[string]any{
		"ok":        true,
		"productos": Almacen.Productos(),
	})
}

// Recepciones → GET /api/demo/recepciones
func (c *DemoController) Recepciones() {
	c.json(http.StatusOK, map[string]any{
		"ok":         true,
		"recepciones": Almacen.Recepciones(),
	})
}

// --- registro de ingreso ---

// CrearRecepcion → POST /api/demo/recepciones
func (c *DemoController) CrearRecepcion() {
	var sol store.SolicitudRecepcion
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &sol); err != nil {
		c.json(http.StatusBadRequest, map[string]any{"ok": false, "estado": store.EstadoError, "mensaje": "JSON inválido"})
		return
	}

	rec, duplicado, err := Almacen.Recepcionar(sol)
	if err != nil {
		estado, status := store.EstadoDesconocido, http.StatusUnprocessableEntity
		if err == store.ErrCantidadInvalida || err == store.ErrCodigoVacio {
			estado, status = store.EstadoError, http.StatusBadRequest
		}
		c.json(status, map[string]any{
			"ok":      false,
			"estado":  estado,
			"codigo":  sol.CodigoProducto,
			"mensaje": err.Error(),
		})
		return
	}

	// La orden se publica solo tras registrar el ingreso. Si MQTT falla, el
	// registro se conserva y queda como clasificación pendiente (reintentable).
	enviado := false
	if !duplicado {
		errPub := MQTT.PublicarClasificacion(rec)
		enviado = errPub == nil
		if errPub != nil {
			Almacen.MarcarClasificacionPendiente(rec.IDOperacion, "Orden MQTT pendiente: "+errPub.Error())
		}
	}

	producto, _ := Almacen.ProductoPorCodigo(rec.CodigoProducto)
	c.json(http.StatusOK, map[string]any{
		"ok":            true,
		"duplicado":     duplicado,
		"recepcion":     rec,
		"producto":      producto,
		"stock":         producto.Stock,
		"mqtt_enviado":  enviado,
		"clasificacion": rec.Clasificacion,
	})
}

// Reintentar → POST /api/demo/reintentar  {id_operacion}
func (c *DemoController) Reintentar() {
	var cuerpo struct {
		IDOperacion string `json:"id_operacion"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil || cuerpo.IDOperacion == "" {
		c.json(http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "Falta id_operacion"})
		return
	}

	rec, ok := Almacen.RecepcionPorOperacion(cuerpo.IDOperacion)
	if !ok {
		c.json(http.StatusNotFound, map[string]any{"ok": false, "mensaje": "Operación no encontrada"})
		return
	}
	if err := MQTT.PublicarClasificacion(rec); err != nil {
		c.json(http.StatusBadGateway, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	c.json(http.StatusOK, map[string]any{"ok": true, "mensaje": "Orden reenviada a la maqueta"})
}

// --- sensor ---

// Sensor → POST /api/demo/sensor  {evento, estado, dispositivo}
func (c *DemoController) Sensor() {
	var ev struct {
		Evento      string `json:"evento"`
		Estado      bool   `json:"estado"`
		Dispositivo string `json:"dispositivo"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &ev); err != nil {
		c.json(http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	nueva := Almacen.RegistrarDeteccion(ev.Dispositivo, ev.Estado)
	c.json(http.StatusOK, map[string]any{
		"ok":     true,
		"nueva":  nueva,
		"sensor": Almacen.EstadoIoT().Sensor,
	})
}

// --- estado e IoT ---

// EstadoIoT → GET /api/demo/estado-iot
func (c *DemoController) EstadoIoT() {
	e := Almacen.EstadoIoT()
	e.MQTTConectado = MQTT.Conectado()
	e.Simulacion = Cfg.Simulacion || MQTT.Simulando()
	c.json(http.StatusOK, map[string]any{"ok": true, "estado": e})
}

// Reiniciar → POST /api/demo/reiniciar
func (c *DemoController) Reiniciar() {
	Almacen.Reiniciar()
	c.json(http.StatusOK, map[string]any{"ok": true, "mensaje": "Demostración reiniciada"})
}

// Health → GET /api/health
func (c *DemoController) Health() {
	c.json(http.StatusOK, map[string]any{
		"status":     "ok",
		"mqtt":       MQTT.Conectado(),
		"simulacion": Cfg.Simulacion || MQTT.Simulando(),
		"ts":         time.Now().UnixMilli(),
	})
}

// --- utilidades ---

func (c *DemoController) json(status int, cuerpo any) {
	c.Ctx.Output.SetStatus(status)
	c.Data["json"] = cuerpo
	c.ServeJSON()
}
