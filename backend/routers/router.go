// Package routers registra las rutas REST y el filtro CORS.
package routers

import (
	"net/http"
	"strings"

	"github.com/beego/beego/v2/server/web"
	"github.com/beego/beego/v2/server/web/context"

	"gestock-iot-backend/controllers"
)

// Init registra el filtro CORS y todas las rutas de la API.
func Init() {
	web.InsertFilter("*", web.BeforeRouter, cors)
	web.InsertFilter("*", web.BeforeRouter, jsonContentType)

	web.Router("/api/health", &controllers.DemoController{}, "get:Health")

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
	ctx.Output.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.Output.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	ctx.Output.Header("Access-Control-Max-Age", "600")

	if ctx.Input.Method() == http.MethodOptions {
		// En Beego, abortar un filtro se hace escribiendo la respuesta y
		// lanzando web.ErrAbort; el recover de Beego lo reconoce y NO lo
		// registra como "handler crashed".
		ctx.ResponseWriter.WriteHeader(http.StatusNoContent)
		panic(web.ErrAbort)
	}
}

// jsonContentType fuerza UTF-8 en las respuestas JSON.
func jsonContentType(ctx *context.Context) {
	ctx.Output.Header("Content-Type", "application/json; charset=utf-8")
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
