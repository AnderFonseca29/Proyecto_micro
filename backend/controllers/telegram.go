package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/store"
	"gestock-iot-backend/telegram"
)

// TelegramController expone la vinculación y las preferencias de Telegram.
type TelegramController struct {
	web.Controller
}

// Estado → GET /api/telegram/estado
func (c *TelegramController) Estado() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "estado": telegram.Estado{Configurado: false}})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "estado": Telegram.Estado(u)})
}

// CrearCodigo → POST /api/telegram/vincular
func (c *TelegramController) CrearCodigo() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "Telegram no está disponible"})
		return
	}
	codigo, expira, enlace, err := Telegram.CrearCodigo(u)
	if err != nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_CODIGO", "Código de vinculación generado"))
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "codigo": codigo, "expira": expira.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"enlace": enlace,
		"instrucciones": "Abre el enlace del bot o envía /start " + codigo + " al bot. El código caduca en 10 minutos y solo puede usarse una vez.",
	})
}

// Desvincular → DELETE /api/telegram/vincular
func (c *TelegramController) Desvincular() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	desvinculado := false
	if Telegram != nil {
		desvinculado = Telegram.Desvincular(u.ID)
	}
	if desvinculado {
		Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_DESVINCULADO", "El usuario desvinculó su cuenta de Telegram"))
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "desvinculado": desvinculado})
}

// Preferencias → PUT /api/telegram/preferencias
func (c *TelegramController) Preferencias() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "Telegram no está disponible"})
		return
	}
	var prefs telegram.Preferencias
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &prefs); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	actualizadas, err := Telegram.ActualizarPreferencias(u.ID, u.Rol, prefs)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, telegram.ErrNoVinculado) {
			status = http.StatusConflict
		}
		responder(&c.Controller, status, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_PREFERENCIAS", "Preferencias de Telegram actualizadas"))
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "preferencias": actualizadas})
}

// Prueba → POST /api/telegram/prueba
func (c *TelegramController) Prueba() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "Telegram no está disponible"})
		return
	}
	if err := Telegram.EnviarPrueba(u.ID); err != nil {
		responder(&c.Controller, http.StatusConflict, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_PRUEBA", "Mensaje de prueba encolado"))
	responder(&c.Controller, http.StatusAccepted, map[string]any{
		"ok": true,
		"mensaje": "Mensaje de prueba encolado. La entrega se confirma cuando Telegram responde; revisa el estado de entregas.",
	})
}

// Webhook → POST /api/telegram/webhook
// Valida el secreto de Telegram. No requiere JWT porque Telegram no lo envía;
// la autenticación es el encabezado X-Telegram-Bot-Api-Secret-Token.
func (c *TelegramController) Webhook() {
	if Telegram == nil || !Telegram.Configurado() {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false})
		return
	}
	secreto := strings.TrimSpace(Cfg.TelegramWebhookSec)
	recibido := strings.TrimSpace(c.Ctx.Input.Header("X-Telegram-Bot-Api-Secret-Token"))
	if secreto == "" || recibido != secreto {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	var update telegram.Update
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &update); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false})
		return
	}
	// Telegram exige 200 para no reintentar indefinidamente. Los errores de
	// negocio se responden al chat, no al webhook.
	_, _ = Telegram.ProcesarWebhook(update)
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true})
}

// Grupo → GET/PUT /api/telegram/grupo (solo telegram.configurar)
func (c *TelegramController) Grupo() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermTelegramConfig) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "grupo": "", "categorias": []string{}})
		return
	}
	grupo, cats := Telegram.Grupo()
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "grupo": grupo, "categorias": cats})
}

// ConfigurarGrupo → PUT /api/telegram/grupo
func (c *TelegramController) ConfigurarGrupo() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermTelegramConfig) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "Telegram no está disponible"})
		return
	}
	var cuerpo struct {
		ChatID     string   `json:"chat_id"`
		Categorias []string `json:"categorias"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	if err := Telegram.ConfigurarGrupo(cuerpo.ChatID, cuerpo.Categorias); err != nil {
		responder(&c.Controller, http.StatusUnprocessableEntity, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_GRUPO", "Configuración del grupo actualizada"))
	grupo, cats := Telegram.Grupo()
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "grupo": grupo, "categorias": cats})
}

// Entregas → GET /api/telegram/entregas (solo telegram.configurar)
func (c *TelegramController) Entregas() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermTelegramConfig) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "entregas": []telegram.Entrega{}})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "entregas": Telegram.Entregas()})
}

// RegistrarWebhook → POST /api/telegram/webhook/registrar (solo admin)
func (c *TelegramController) RegistrarWebhook() {
	u, ok := usuarioActual(&c.Controller)
	if !ok || !u.TienePermiso(auth.PermTelegramConfig) {
		responder(&c.Controller, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no autorizado"})
		return
	}
	if Telegram == nil {
		responder(&c.Controller, http.StatusServiceUnavailable, map[string]any{"ok": false, "mensaje": "Telegram no está disponible"})
		return
	}
	if err := Telegram.RegistrarWebhook(); err != nil {
		responder(&c.Controller, http.StatusUnprocessableEntity, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	Almacen.RegistrarAuditoria(storeAuditoria(u, c.Ctx.Input.IP(), "TELEGRAM_WEBHOOK", "Webhook registrado en Telegram"))
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "mensaje": "Webhook registrado en Telegram"})
}

// helper local para auditoría de acciones de Telegram.
func storeAuditoria(u auth.Usuario, ip, evento, detalle string) store.Auditoria {
	return store.Auditoria{
		Evento: evento, Detalle: detalle, Usuario: u.Nombre, Rol: string(u.Rol),
		IP: ip, Severidad: "INFO",
	}
}
