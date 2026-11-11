package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/ai"
	"gestock-iot-backend/auth"
)

// IAController expone GESTOCK AI. Nunca recibe claves del proveedor.
type IAController struct {
	web.Controller
}

// Estado → GET /api/ia/estado
func (c *IAController) Estado() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermIAUsar) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if IA == nil {
		responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "estado": ai.Estado{Disponible: false, Modo: "desactivado"}})
		return
	}
	estado := IA.Estado()
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "estado": estado})
}

// Chat → POST /api/ia/chat
// Cuerpo: {"mensaje":"..."} o {"mensajes":[{"rol":"usuario","contenido":"..."}]}
func (c *IAController) Chat() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermIAUsar) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if IA == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "GESTOCK AI no está disponible"})
		return
	}
	var cuerpo struct {
		Mensaje  string       `json:"mensaje"`
		Mensajes []ai.Mensaje `json:"mensajes"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	historial := cuerpo.Mensajes
	if strings.TrimSpace(cuerpo.Mensaje) != "" {
		historial = append(historial, ai.Mensaje{Rol: "usuario", Contenido: cuerpo.Mensaje})
	}
	if len(historial) == 0 {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "escribe una pregunta"})
		return
	}
	timeout := Cfg.AITimeout + 5*time.Second
	if timeout < 15*time.Second {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Ctx.Request.Context(), timeout)
	defer cancel()
	respuesta, err := IA.Chat(ctx, u, historial)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "límite de consultas") {
			status = http.StatusTooManyRequests
		}
		responder(&c.Controller, status, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "agente": respuesta})
}

// Sugerencias → GET /api/ia/sugerencias
func (c *IAController) Sugerencias() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermIAUsar) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "sugerencias": ai.Sugerencias})
}
