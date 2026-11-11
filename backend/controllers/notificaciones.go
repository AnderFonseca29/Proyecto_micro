package controllers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/notifications"
)

// NotificacionesController expone el centro de notificaciones interno.
type NotificacionesController struct {
	web.Controller
}

// Listar → GET /api/notificaciones?filtro=&categoria=&q=&orden=
func (c *NotificacionesController) Listar() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para ver notificaciones"})
		return
	}
	lista := Notificaciones.Listar(u, c.GetString("filtro"), strings.ToUpper(c.GetString("categoria")), c.GetString("q"))
	if c.GetString("orden") == "prioridad" {
		notifications.OrdenarPorPrioridad(lista)
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "notificaciones": lista, "no_leidas": Notificaciones.Contador(u),
	})
}

// Contador → GET /api/notificaciones/contador
func (c *NotificacionesController) Contador() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "no_leidas": Notificaciones.Contador(u)})
}

// Obtener → GET /api/notificaciones/:id
func (c *NotificacionesController) Obtener() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	n, encontrada := Notificaciones.Obtener(u, c.Ctx.Input.Param(":id"))
	if !encontrada {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "notificación no encontrada"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "notificacion": n})
}

// Leer → POST /api/notificaciones/:id/leer  {leida:true|false}
func (c *NotificacionesController) Leer() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	cuerpo := struct {
		Leida *bool `json:"leida"`
	}{}
	_ = json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo)
	leida := true
	if cuerpo.Leida != nil {
		leida = *cuerpo.Leida
	}
	if !Notificaciones.MarcarLeida(u, c.Ctx.Input.Param(":id"), leida) {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "notificación no encontrada"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "no_leidas": Notificaciones.Contador(u)})
}

// LeerTodas → POST /api/notificaciones/leer-todas
func (c *NotificacionesController) LeerTodas() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	total := Notificaciones.MarcarTodas(u)
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "marcadas": total, "no_leidas": 0})
}

// Archivar → DELETE /api/notificaciones/:id (archivado por usuario)
func (c *NotificacionesController) Archivar() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesLeer) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if !Notificaciones.Archivar(u, c.Ctx.Input.Param(":id")) {
		responder(&c.Controller, http.StatusNotFound, map[string]any{"ok": false, "mensaje": "notificación no encontrada"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true})
}

// Crear → POST /api/notificaciones (solo notificaciones.gestionar)
func (c *NotificacionesController) Crear() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermNotificacionesGestionar) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	var in notifications.Entrada
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &in); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	if strings.TrimSpace(in.Titulo) == "" {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "el título es obligatorio"})
		return
	}
	n := Notificaciones.Crear(in)
	if n == nil {
		responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "duplicada": true})
		return
	}
	responder(&c.Controller, http.StatusCreated, map[string]any{"ok": true, "notificacion": n})
}
