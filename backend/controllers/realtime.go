package controllers

import (
	"net/http"

	"github.com/beego/beego/v2/server/web"
)

// RealtimeController entrega el ticket efímero y actualiza el WebSocket.
type RealtimeController struct {
	web.Controller
}

// Ticket → POST /api/ws/ticket
// El JWT viaja en la cabecera Authorization; el ticket resultante se usa una
// sola vez y caduca en 60 segundos.
func (c *RealtimeController) Ticket() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	if Hub == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "tiempo real desactivado"})
		return
	}
	ticket := Hub.CrearTicket(u)
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "ticket": ticket, "expira_en_segundos": 60,
	})
}

// Abrir → GET /api/ws?ticket=...
func (c *RealtimeController) Abrir() {
	if Hub == nil {
		http.Error(c.Ctx.ResponseWriter, "tiempo real desactivado", http.StatusServiceUnavailable)
		return
	}
	Hub.ServeWS(c.Ctx.ResponseWriter, c.Ctx.Request, c.GetString("ticket"))
}
