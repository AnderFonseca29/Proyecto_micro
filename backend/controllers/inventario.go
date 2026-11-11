package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/store"
)

// InventarioController agrupa inventario, movimientos, mantenimiento y
// auditoría. Todas las operaciones validan permisos en el backend.
type InventarioController struct {
	web.Controller
}

// Resumen → GET /api/inventario/resumen
func (c *InventarioController) Resumen() {
	if !autorizado(&c.Controller, auth.PermInventarioLeer) {
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok":            true,
		"resumen":       Almacen.Resumen(),
		"stock_bajo":    Almacen.ProductosStockBajo(),
		"estado_iot":    Almacen.EstadoIoT(),
		"movimientos":   Almacen.Movimientos("", "", 8),
		"recepciones":   Almacen.RecepcionesFiltradas("", "", 8),
		"incidencias":   Almacen.Incidencias(),
	})
}

// Productos → GET /api/inventario/productos?q=
func (c *InventarioController) Productos() {
	if !autorizado(&c.Controller, auth.PermInventarioLeer) {
		return
	}
	q := c.GetString("q")
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "productos": Almacen.BuscarProductos(q),
	})
}

// Producto → GET /api/inventario/productos/:codigo
func (c *InventarioController) Producto() {
	if !autorizado(&c.Controller, auth.PermInventarioLeer) {
		return
	}
	codigo := c.Ctx.Input.Param(":codigo")
	p, ok := Almacen.ProductoPorCodigo(codigo)
	if !ok {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "producto no encontrado"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "producto": p,
		"movimientos": Almacen.Movimientos("", p.Codigo, 20),
		"recepciones": Almacen.RecepcionesFiltradas("", p.Codigo, 20),
	})
}

// StockBajo → GET /api/inventario/stock-bajo
func (c *InventarioController) StockBajo() {
	if !autorizado(&c.Controller, auth.PermInventarioLeer) {
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "productos": Almacen.ProductosStockBajo(),
	})
}

// Movimientos → GET /api/inventario/movimientos
func (c *InventarioController) Movimientos() {
	if !autorizado(&c.Controller, auth.PermMovimientosLeer) {
		return
	}
	limite, _ := strconv.Atoi(c.GetString("limite"))
	movs := Almacen.Movimientos(c.GetString("tipo"), c.GetString("codigo"), limite)
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "movimientos": movs})
}

// CrearMovimiento → POST /api/inventario/movimientos
// Solo permite SALIDA y AJUSTE. Las entradas se registran por recepción para
// mantener trazabilidad IoT.
func (c *InventarioController) CrearMovimiento() {
	if !autorizado(&c.Controller, auth.PermInventarioEscribir) {
		return
	}
	u, _ := usuarioActual(&c.Controller)
	var cuerpo struct {
		CodigoProducto string `json:"codigo_producto"`
		Tipo           string `json:"tipo"`
		Cantidad       int    `json:"cantidad"`
		StockNuevo     *int   `json:"stock_nuevo"`
		Motivo         string `json:"motivo"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	tipo := strings.ToUpper(strings.TrimSpace(cuerpo.Tipo))
	if tipo != store.MovSalida && tipo != store.MovAjuste {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "solo se permiten movimientos SALIDA o AJUSTE"})
		return
	}
	sol := store.SolicitudMovimiento{
		CodigoProducto: strings.TrimSpace(cuerpo.CodigoProducto),
		Cantidad:       cuerpo.Cantidad,
		StockNuevo:     cuerpo.StockNuevo,
		Motivo:         cuerpo.Motivo,
		Usuario:        u.Nombre,
		Rol:            string(u.Rol),
	}
	anterior := 0
	if p, ok := Almacen.ProductoPorCodigo(sol.CodigoProducto); ok {
		anterior = p.Stock
	}
	producto, movimiento, err := Almacen.AplicarMovimiento(sol, tipo)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if err == store.ErrCantidadInvalida || err == store.ErrStockInvalido {
			status = http.StatusBadRequest
		}
		responder(&c.Controller, status, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}

	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "MOVIMIENTO_" + tipo,
		Detalle: fmt.Sprintf("%s %s: %d → %d (%s)", tipo, producto.Codigo, anterior, producto.Stock, movimiento.Motivo),
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "MEDIA",
	})
	if Hub != nil {
		Hub.Emitir("movimiento", movimiento, func(us auth.Usuario) bool {
			return us.TienePermiso(auth.PermMovimientosLeer)
		})
	}
	evaluarStock(producto, anterior)
	if tipo == store.MovAjuste && Notificaciones != nil {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatAuditoria, Prioridad: notifications.PrioridadMedia,
			Titulo: "Ajuste de inventario registrado",
			Mensaje: fmt.Sprintf("%s ajustó %s de %d a %d uds. Motivo: %s.", u.Nombre, producto.Nombre, anterior, producto.Stock, valorTexto(movimiento.Motivo, "sin motivo")),
			Modulo: "/inventario", Entidad: producto.Codigo,
			Roles: []string{string(auth.RolAdmin), string(auth.RolAuditor)},
			Clave: "ajuste-" + producto.Codigo,
		})
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "producto": producto, "movimiento": movimiento})
}

// Recepciones → GET /api/inventario/recepciones
func (c *InventarioController) Recepciones() {
	if !autorizado(&c.Controller, auth.PermRecepcionLeer) {
		return
	}
	limite, _ := strconv.Atoi(c.GetString("limite"))
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "recepciones": Almacen.RecepcionesFiltradas(c.GetString("estado"), c.GetString("q"), limite),
		"pendientes": Almacen.Pendientes(),
	})
}

// AlertasIoT → GET /api/inventario/alertas-iot
func (c *InventarioController) AlertasIoT() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !puedeVerIoT(u) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para consultar IoT"})
		return
	}
	estado := Almacen.EstadoIoT()
	if MQTT != nil {
		estado.MQTTConectado = MQTT.Conectado()
		estado.Simulacion = estado.Simulacion || MQTT.Simulando()
	}
	fallidas := make([]store.Recepcion, 0)
	for _, r := range Almacen.Recepciones() {
		if r.Clasificacion == store.ClasificacionFallida {
			fallidas = append(fallidas, r)
			if len(fallidas) >= 20 {
				break
			}
		}
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "estado": estado, "clasificaciones_fallidas": fallidas,
		"pendientes": Almacen.Pendientes(),
	})
}

// Incidencias → GET /api/mantenimiento/incidencias
func (c *InventarioController) Incidencias() {
	if !autorizado(&c.Controller, auth.PermMantenimientoLeer) {
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "incidencias": Almacen.Incidencias()})
}

// CrearIncidencia → POST /api/mantenimiento/incidencias
func (c *InventarioController) CrearIncidencia() {
	if !autorizado(&c.Controller, auth.PermMantenimientoEscribir) {
		return
	}
	u, _ := usuarioActual(&c.Controller)
	var cuerpo struct {
		Titulo      string `json:"titulo"`
		Descripcion string `json:"descripcion"`
		Prioridad   string `json:"prioridad"`
		Dispositivo string `json:"dispositivo"`
		AsignadaA   string `json:"asignada_a"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	if strings.TrimSpace(cuerpo.Titulo) == "" {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "el título es obligatorio"})
		return
	}
	sol := store.SolicitudIncidencia{
		Titulo: cuerpo.Titulo, Descripcion: cuerpo.Descripcion, Prioridad: cuerpo.Prioridad,
		Dispositivo: cuerpo.Dispositivo, AsignadaA: cuerpo.AsignadaA, CreadaPor: u.Nombre,
	}
	in := Almacen.CrearIncidencia(sol)
	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "INCIDENCIA_CREADA", Detalle: fmt.Sprintf("#%d %s", in.ID, in.Titulo),
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "MEDIA",
	})
	prioridad := notifications.PrioridadAlta
	if in.Prioridad == "CRITICA" {
		prioridad = notifications.PrioridadCritica
	}
	if Notificaciones != nil {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatMantenimiento, Prioridad: prioridad,
			Titulo: "Nueva incidencia de mantenimiento",
			Mensaje: fmt.Sprintf("#%d %s — %s. Dispositivo: %s.", in.ID, in.Titulo, in.Descripcion, valorTexto(in.Dispositivo, "no especificado")),
			Modulo: "/mantenimiento", Entidad: fmt.Sprintf("INC-%d", in.ID),
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)},
			Clave: fmt.Sprintf("incidencia-%d", in.ID),
		})
	}
	if Hub != nil {
		Hub.Emitir("incidencia", in, func(us auth.Usuario) bool {
			return us.TienePermiso(auth.PermMantenimientoLeer)
		})
	}
	responder(&c.Controller, http.StatusCreated, map[string]any{"ok": true, "incidencia": in})
}

// CambiarIncidencia → PATCH /api/mantenimiento/incidencias/:id
func (c *InventarioController) CambiarIncidencia() {
	if !autorizado(&c.Controller, auth.PermMantenimientoEscribir) {
		return
	}
	u, _ := usuarioActual(&c.Controller)
	id, err := strconv.ParseInt(c.Ctx.Input.Param(":id"), 10, 64)
	if err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "id inválido"})
		return
	}
	var cambio store.CambioIncidencia
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cambio); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	in, err := Almacen.CambiarIncidencia(id, cambio)
	if err != nil {
		responder(&c.Controller, http.StatusUnprocessableEntity, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "INCIDENCIA_ACTUALIZADA", Detalle: fmt.Sprintf("#%d → %s", in.ID, in.Estado),
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "MEDIA",
	})
	if in.Estado == "RESUELTA" && Notificaciones != nil {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatMantenimiento, Prioridad: notifications.PrioridadMedia,
			Titulo: "Incidencia resuelta",
			Mensaje: fmt.Sprintf("#%d %s fue resuelta por %s. %s", in.ID, in.Titulo, u.Nombre, valorTexto(in.Resolucion, "")),
			Modulo: "/mantenimiento", Entidad: fmt.Sprintf("INC-%d", in.ID),
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)},
			Clave: fmt.Sprintf("incidencia-resuelta-%d", in.ID),
		})
	}
	if Hub != nil {
		Hub.Emitir("incidencia", in, func(us auth.Usuario) bool {
			return us.TienePermiso(auth.PermMantenimientoLeer)
		})
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "incidencia": in})
}

// Auditoria → GET /api/inventario/auditoria
func (c *InventarioController) Auditoria() {
	if !autorizado(&c.Controller, auth.PermAuditoriaLeer) {
		return
	}
	limite, _ := strconv.Atoi(c.GetString("limite"))
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "eventos": Almacen.Auditoria(limite)})
}

// evaluarStock genera alertas de inventario e IA con datos confirmados.
func evaluarStock(p store.Producto, anterior int) {
	if Notificaciones == nil {
		return
	}
	if p.Stock == 0 {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatInventario, Prioridad: notifications.PrioridadCritica,
			Titulo: "Producto agotado",
			Mensaje: fmt.Sprintf("%s (%s) quedó sin existencias. Stock mínimo: %d.", p.Nombre, p.Codigo, p.StockMinimo),
			Modulo: "/inventario", Entidad: p.Codigo,
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor)},
			Clave: "agotado-" + p.Codigo,
		})
		faltan := p.StockMinimo
		if faltan < 1 {
			faltan = 1
		}
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatIA, Prioridad: notifications.PrioridadAlta,
			Titulo: "Riesgo de agotamiento detectado",
			Mensaje: fmt.Sprintf("Análisis con datos reales: %s está agotado. Recomendación: reponer al menos %d uds para volver al mínimo.", p.Nombre, faltan),
			Modulo: "/inventario", Entidad: p.Codigo,
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor)},
			Clave: "ia-agotado-" + p.Codigo,
		})
		return
	}
	if p.Stock <= p.StockMinimo && anterior > p.StockMinimo {
		faltan := p.StockMinimo - p.Stock
		if faltan < 1 {
			faltan = 1
		}
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatInventario, Prioridad: notifications.PrioridadAlta,
			Titulo: "Stock bajo",
			Mensaje: fmt.Sprintf("%s (%s) tiene %d uds y el mínimo es %d. Faltan %d para el mínimo.", p.Nombre, p.Codigo, p.Stock, p.StockMinimo, faltan),
			Modulo: "/inventario", Entidad: p.Codigo,
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor)},
			Clave: "stock-bajo-" + p.Codigo,
		})
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatIA, Prioridad: notifications.PrioridadMedia,
			Titulo: "Reposición recomendada",
			Mensaje: fmt.Sprintf("Basado en el stock actual de %s (%d uds, mínimo %d), la recomendación es reponer %d uds. Recomendación no confirmada.", p.Nombre, p.Stock, p.StockMinimo, faltan),
			Modulo: "/inventario", Entidad: p.Codigo,
			Roles: []string{string(auth.RolAdmin), string(auth.RolSupervisor)},
			Clave: "ia-reposicion-" + p.Codigo,
		})
	}
}

func valorTexto(v, defecto string) string {
	if strings.TrimSpace(v) == "" {
		return defecto
	}
	return v
}
