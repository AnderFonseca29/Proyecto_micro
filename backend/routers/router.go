// Package routers registra las rutas REST, el filtro CORS y la autenticación
// JWT. El WebSocket se autentica con un ticket de un solo uso, nunca con el
// JWT en la URL.
package routers

import (
	"net/http"
	"strings"

	"github.com/beego/beego/v2/server/web"
	"github.com/beego/beego/v2/server/web/context"

	"gestock-iot-backend/controllers"
)

// Init registra los filtros y todas las rutas de la API.
func Init() {
	web.InsertFilter("*", web.BeforeRouter, cors)
	web.InsertFilter("*", web.BeforeRouter, jsonContentType)
	web.InsertFilter("*", web.BeforeRouter, autenticacion)

	// --- públicos ---
	web.Router("/api/health", &controllers.DemoController{}, "get:Health")
	web.Router("/api/auth/login", &controllers.AuthController{}, "post:Login")
	web.Router("/api/telegram/webhook", &controllers.TelegramController{}, "post:Webhook")

	// --- sesión y usuarios ---
	web.Router("/api/auth/me", &controllers.AuthController{}, "get:Me")
	web.Router("/api/auth/logout", &controllers.AuthController{}, "post:Logout")
	web.Router("/api/auth/permisos", &controllers.AuthController{}, "get:Permisos")
	web.Router("/api/usuarios", &controllers.AuthController{}, "get:Usuarios")

	// --- tiempo real ---
	web.Router("/api/ws/ticket", &controllers.RealtimeController{}, "post:Ticket")
	web.Router("/api/ws", &controllers.RealtimeController{}, "get:Abrir")

	// --- notificaciones (rutas específicas antes de :id) ---
	web.Router("/api/notificaciones", &controllers.NotificacionesController{}, "get:Listar;post:Crear")
	web.Router("/api/notificaciones/contador", &controllers.NotificacionesController{}, "get:Contador")
	web.Router("/api/notificaciones/leer-todas", &controllers.NotificacionesController{}, "post:LeerTodas")
	web.Router("/api/notificaciones/:id/leer", &controllers.NotificacionesController{}, "post:Leer")
	web.Router("/api/notificaciones/:id", &controllers.NotificacionesController{}, "get:Obtener;delete:Archivar")

	// --- inventario ---
	web.Router("/api/inventario/resumen", &controllers.InventarioController{}, "get:Resumen")
	web.Router("/api/inventario/productos", &controllers.InventarioController{}, "get:Productos")
	web.Router("/api/inventario/productos/:codigo", &controllers.InventarioController{}, "get:Producto")
	web.Router("/api/inventario/stock-bajo", &controllers.InventarioController{}, "get:StockBajo")
	web.Router("/api/inventario/movimientos", &controllers.InventarioController{}, "get:Movimientos;post:CrearMovimiento")
	web.Router("/api/inventario/recepciones", &controllers.InventarioController{}, "get:Recepciones")
	web.Router("/api/inventario/alertas-iot", &controllers.InventarioController{}, "get:AlertasIoT")
	web.Router("/api/inventario/auditoria", &controllers.InventarioController{}, "get:Auditoria")

	// --- mantenimiento ---
	web.Router("/api/mantenimiento/incidencias", &controllers.InventarioController{}, "get:Incidencias;post:CrearIncidencia")
	web.Router("/api/mantenimiento/incidencias/:id", &controllers.InventarioController{}, "patch:CambiarIncidencia")

	// --- Telegram ---
	web.Router("/api/telegram/estado", &controllers.TelegramController{}, "get:Estado")
	web.Router("/api/telegram/vincular", &controllers.TelegramController{}, "post:CrearCodigo;delete:Desvincular")
	web.Router("/api/telegram/preferencias", &controllers.TelegramController{}, "put:Preferencias")
	web.Router("/api/telegram/prueba", &controllers.TelegramController{}, "post:Prueba")
	web.Router("/api/telegram/webhook/registrar", &controllers.TelegramController{}, "post:RegistrarWebhook")
	web.Router("/api/telegram/grupo", &controllers.TelegramController{}, "get:Grupo;put:ConfigurarGrupo")
	web.Router("/api/telegram/entregas", &controllers.TelegramController{}, "get:Entregas")

	// --- GESTOCK AI ---
	web.Router("/api/ia/estado", &controllers.IAController{}, "get:Estado")
	web.Router("/api/ia/chat", &controllers.IAController{}, "post:Chat")
	web.Router("/api/ia/sugerencias", &controllers.IAController{}, "get:Sugerencias")

	// --- reportes (tipos antes de :id) ---
	web.Router("/api/reportes/tipos", &controllers.ReportesController{}, "get:Tipos")
	web.Router("/api/reportes", &controllers.ReportesController{}, "post:Generar")
	web.Router("/api/reportes/:id", &controllers.ReportesController{}, "get:Descargar")

	// --- endpoints de demostración existentes, ahora autenticados ---
	web.Router("/api/demo/productos", &controllers.DemoController{}, "get:Productos")
	web.Router("/api/demo/recepciones", &controllers.DemoController{}, "get:Recepciones;post:CrearRecepcion")
	web.Router("/api/demo/reintentar", &controllers.DemoController{}, "post:Reintentar")
	web.Router("/api/demo/sensor", &controllers.DemoController{}, "post:Sensor")
	web.Router("/api/demo/estado-iot", &controllers.DemoController{}, "get:EstadoIoT")
	web.Router("/api/demo/reiniciar", &controllers.DemoController{}, "post:Reiniciar")
}

// cors aplica las cabeceras CORS y responde los preflight (OPTIONS).
func cors(ctx *context.Context) {
	origen := ctx.Input.Header("Origin")
	permitido := origenPermitido(origen)
	if permitido == "" {
		permitido = "null"
	}
	ctx.Output.Header("Access-Control-Allow-Origin", permitido)
	ctx.Output.Header("Vary", "Origin")
	ctx.Output.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	ctx.Output.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
	ctx.Output.Header("Access-Control-Max-Age", "600")

	if ctx.Input.Method() == http.MethodOptions {
		ctx.ResponseWriter.WriteHeader(http.StatusNoContent)
		panic(web.ErrAbort)
	}
}

// jsonContentType fuerza UTF-8 en las respuestas JSON. El WebSocket necesita
// conservar sus propias cabeceras de actualización.
func jsonContentType(ctx *context.Context) {
	if strings.HasPrefix(ctx.Request.URL.Path, "/api/ws") {
		return
	}
	ctx.Output.Header("Content-Type", "application/json; charset=utf-8")
}

// autenticacion valida el JWT para toda la API salvo las rutas públicas.
// El ticket de WebSocket es la autenticación de /api/ws.
func autenticacion(ctx *context.Context) {
	ruta := strings.TrimRight(ctx.Request.URL.Path, "/")
	if ruta == "" {
		ruta = "/"
	}
	if !strings.HasPrefix(ruta, "/api/") || esPublica(ruta) {
		return
	}

	cabecera := strings.TrimSpace(ctx.Input.Header("Authorization"))
	if !strings.HasPrefix(strings.ToLower(cabecera), "bearer ") {
		rechazar(ctx, http.StatusUnauthorized, "falta el token de sesión")
	}
	token := strings.TrimSpace(cabecera[len("Bearer "):])
	if controllers.Auth == nil {
		rechazar(ctx, http.StatusServiceUnavailable, "autenticación no inicializada")
	}
	usuario, err := controllers.Auth.Validar(token)
	if err != nil {
		rechazar(ctx, http.StatusUnauthorized, "sesión inválida o expirada")
	}
	ctx.Input.SetData("usuario", usuario)
}

func esPublica(ruta string) bool {
	switch ruta {
	case "/api/health", "/api/auth/login", "/api/telegram/webhook", "/api/ws":
		return true
	}
	return false
}

func rechazar(ctx *context.Context, estado int, mensaje string) {
	ctx.Output.Header("Content-Type", "application/json; charset=utf-8")
	ctx.Output.SetStatus(estado)
	_ = ctx.Output.Body([]byte(`{"ok":false,"mensaje":"` + mensaje + `"}`))
	panic(web.ErrAbort)
}

func origenPermitido(origen string) string {
	if origen == "" {
		return ""
	}
	for _, permitido := range controllers.Cfg.CorsOrigenes {
		if permitido == "*" {
			return origen
		}
		if strings.EqualFold(strings.TrimRight(permitido, "/"), strings.TrimRight(origen, "/")) {
			return origen
		}
	}
	return ""
}
