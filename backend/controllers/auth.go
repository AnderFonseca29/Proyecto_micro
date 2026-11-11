package controllers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/store"
)

// AuthController gestiona sesión, perfil y consulta de usuarios.
type AuthController struct {
	web.Controller
}

// Login → POST /api/auth/login
func (c *AuthController) Login() {
	var cuerpo struct {
		Usuario  string `json:"usuario"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(c.Ctx.Input.RequestBody, &cuerpo); err != nil {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "JSON inválido"})
		return
	}
	cuerpo.Usuario = strings.TrimSpace(cuerpo.Usuario)
	if cuerpo.Usuario == "" || cuerpo.Password == "" {
		responder(&c.Controller, http.StatusBadRequest, map[string]any{"ok": false, "mensaje": "usuario y contraseña son obligatorios"})
		return
	}
	ip := c.Ctx.Input.IP()
	u, token, expira, err := Auth.Autenticar(cuerpo.Usuario, cuerpo.Password, ip)
	if err != nil {
		fallos := Auth.FallosRecientes(cuerpo.Usuario, ip)
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: "LOGIN_FALLIDO", Detalle: "Intento de inicio de sesión fallido",
			Usuario: cuerpo.Usuario, Rol: "DESCONOCIDO", IP: ip, Severidad: "MEDIA",
		})
		if fallos >= 3 && Notificaciones != nil {
			Notificaciones.Crear(notifications.Entrada{
				Categoria: notifications.CatSeguridad,
				Prioridad: notifications.PrioridadAlta,
				Titulo:    "Inicios de sesión fallidos repetidos",
				Mensaje:   "Se detectaron varios intentos fallidos para el usuario " + cuerpo.Usuario + " desde " + ip + ". Revisa si es actividad legítima.",
				Modulo:    "/notificaciones",
				Roles:     []string{string(auth.RolAdmin), string(auth.RolAuditor)},
				Clave:     "login-fallos-" + strings.ToLower(cuerpo.Usuario),
			})
		}
		// Mensaje genérico: no revela si el usuario existe.
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}

	Almacen.RegistrarAuditoria(store.Auditoria{
		Evento: "LOGIN", Detalle: "Inicio de sesión correcto",
		Usuario: u.Nombre, Rol: string(u.Rol), IP: ip, Severidad: "INFO",
	})
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok":         true,
		"token":      token,
		"expira":     expira.UTC().Format(time.RFC3339),
		"usuario":    u.Publico(),
	})
}

// Me → GET /api/auth/me
func (c *AuthController) Me() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "usuario": u.Publico()})
}

// Logout → POST /api/auth/logout (JWT sin estado: se audita el cierre).
func (c *AuthController) Logout() {
	if u, ok := usuarioActual(&c.Controller); ok {
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: "LOGOUT", Detalle: "Cierre de sesión solicitado por el usuario",
			Usuario: u.Nombre, Rol: string(u.Rol), IP: c.Ctx.Input.IP(), Severidad: "INFO",
		})
	}
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true})
}

// Usuarios → GET /api/usuarios (solo usuarios.leer)
func (c *AuthController) Usuarios() {
	if !autorizado(&c.Controller, auth.PermUsuariosLeer) {
		return
	}
	usuarios := Auth.Listar()
	responder(&c.Controller, http.StatusOK, map[string]any{"ok": true, "usuarios": usuarios})
}

// Permisos → GET /api/auth/permisos; catálogo para la UI.
func (c *AuthController) Permisos() {
	u, ok := usuarioActual(&c.Controller)
	if !ok {
		responder(&c.Controller, http.StatusUnauthorized, map[string]any{"ok": false, "mensaje": "sesión no válida"})
		return
	}
	responder(&c.Controller, http.StatusOK, map[string]any{
		"ok": true, "rol": u.Rol, "permisos": u.Permisos(),
	})
}
