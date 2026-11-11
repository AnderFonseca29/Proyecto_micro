// Package controllers expone la API REST de GESTOCK. Los controladores no
// contienen reglas de negocio: delegan en store, auth, IA, Telegram y
// notificaciones, y validan permisos en cada endpoint.
package controllers

import (
	"net/http"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/ai"
	"gestock-iot-backend/auth"
	"gestock-iot-backend/broker"
	"gestock-iot-backend/config"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/realtime"
	"gestock-iot-backend/reports"
	"gestock-iot-backend/store"
	"gestock-iot-backend/telegram"
)

// Dependencias inyectadas desde main.
var (
	Almacen        *store.Store
	MQTT           *broker.Broker
	Cfg            config.Config
	Auth           *auth.Servicio
	Hub            *realtime.Hub
	Notificaciones *notifications.Service
	Telegram       *telegram.Servicio
	IA             *ai.Servicio
	Reportes       *reports.Servicio
)

// Inicializar asigna todas las dependencias una sola vez al arrancar.
func Inicializar(
	st *store.Store,
	mqtt *broker.Broker,
	cfg config.Config,
	authSvc *auth.Servicio,
	hub *realtime.Hub,
	notif *notifications.Service,
	tg *telegram.Servicio,
	ia *ai.Servicio,
	rep *reports.Servicio,
) {
	Almacen = st
	MQTT = mqtt
	Cfg = cfg
	Auth = authSvc
	Hub = hub
	Notificaciones = notif
	Telegram = tg
	IA = ia
	Reportes = rep
}

// usuarioActual recupera el usuario autenticado colocado por el filtro JWT.
func usuarioActual(c *web.Controller) (auth.Usuario, bool) {
	valor := c.Ctx.Input.GetData("usuario")
	u, ok := valor.(auth.Usuario)
	return u, ok
}

// autorizado escribe un 403 uniforme si falta el permiso. Devuelve true si
// el usuario puede continuar.
func autorizado(c *web.Controller, permiso auth.Permiso) bool {
	u, ok := usuarioActual(c)
	if !ok {
		responder(c, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return false
	}
	if !u.TienePermiso(permiso) {
		responder(c, http.StatusForbidden, map[string]any{"ok": false, "mensaje": "no tienes permiso para esta operación"})
		return false
	}
	return true
}

// responder escribe JSON y evita repetir el patrón en cada controlador.
func responder(c *web.Controller, status int, cuerpo any) {
	c.Ctx.Output.SetStatus(status)
	c.Data["json"] = cuerpo
	c.ServeJSON()
}

// puedeVerIoT permite a operarios ver el sensor para la recepción, además de
// los roles con permiso iot.leer.
func puedeVerIoT(u auth.Usuario) bool {
	return u.TienePermiso(auth.PermIoTLeer) || u.TienePermiso(auth.PermRecepcionLeer)
}

// emitirEstadoIoT difunde el estado consolidado a quienes pueden verlo.
func emitirEstadoIoT() {
	if Hub == nil || Almacen == nil {
		return
	}
	e := Almacen.EstadoIoT()
	if MQTT != nil {
		e.MQTTConectado = MQTT.Conectado()
		if MQTT.Simulando() {
			e.Simulacion = true
		}
	}
	Hub.Emitir("iot_estado", e, puedeVerIoT)
}

// emitirRecepcion difunde una recepción registrada o actualizada.
func emitirRecepcion(rec store.Recepcion) {
	if Hub == nil {
		return
	}
	Hub.Emitir("recepcion", rec, func(u auth.Usuario) bool {
		return u.TienePermiso(auth.PermRecepcionLeer) || u.TienePermiso(auth.PermIoTLeer)
	})
}
