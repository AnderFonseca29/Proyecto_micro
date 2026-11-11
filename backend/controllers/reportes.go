package controllers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/reports"
	"gestock-iot-backend/store"
)

// ReportesController genera y descarga reportes CSV verificables.
type ReportesController struct {
	web.Controller
}

var tiposReporte = []map[string]string{
	{"tipo": "inventario", "nombre": "Inventario completo"},
	{"tipo": "stock_bajo", "nombre": "Productos con stock bajo"},
	{"tipo": "movimientos", "nombre": "Movimientos de inventario"},
	{"tipo": "recepciones", "nombre": "Recepciones"},
	{"tipo": "incidencias", "nombre": "Incidencias de mantenimiento"},
	{"tipo": "auditoria", "nombre": "Auditoría"},
}

// Tipos → GET /api/reportes/tipos
func (c *ReportesController) Tipos() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || (!u.TienePermiso(auth.PermReportesLeer) && !u.TienePermiso(auth.PermReportesGenerar)) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "tipos": tiposReporte})
}

// Generar → POST /api/reportes  {"tipo":"inventario"}
func (c *ReportesController) Generar() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermReportesGenerar) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	var cuerpo struct {
		Tipo string `json:"tipo"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	tipo := strings.ToLower(strings.TrimSpace(cuerpo.Tipo))
	// La auditoría requiere permiso específico aunque el rol pueda generar reportes.
	if tipo == "auditoria" && !u.TienePermiso(auth.PermAuditoriaLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para reportes de auditoría"})
		return
	}
	nombre, contenido, err := reports.Generar(tipo, Almacen)
	if err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	r := Reportes.Crear(nombre, contenido, u.ID)
	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "REPORTE_GENERADO", Detalle: "Reporte " + tipo + " (" + nombre + ")",
		Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "INFO",
	})
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "reporte": map[string]any{
			"id": r.ID, "nombre": r.Nombre, "mime": r.MIME,
			"generado_en": r.Creado.Format("2006-01-02T15:04:05Z07:00"),
			"descarga":    "/api/reportes/" + r.ID,
			"filtros":     "ninguno",
		},
	})
}

// Descargar → GET /api/reportes/:id
func (c *ReportesController) Descargar() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || (!u.TienePermiso(auth.PermReportesLeer) && !u.TienePermiso(auth.PermReportesGenerar)) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	r, encontrado := Reportes.Obtener(c.Ctx.Input.Param(":id"), u.ID, u.Rol == auth.RolAdmin)
	if !encontrado {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "reporte no encontrado o expirado"})
		return
	}
	c.Ctx.Output.Header("Content-Type", r.MIME)
	c.Ctx.Output.Header("Content-Disposition", `attachment; filename="`+r.Nombre+`"`)
	c.Ctx.Output.Header("Cache-Control", "no-store")
	c.Ctx.Output.SetStatus(http.StatusOK)
	_, _ = c.Ctx.ResponseWriter.Write(r.Contenido)
}
