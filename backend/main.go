// GESTOCK · Recepción Inteligente IoT — backend Go/Beego.
//
// Servicio independiente que actúa como fuente de verdad de los datos
// temporales de la demostración (catálogo, stock e historial en memoria) y
// como puente MQTT entre la interfaz Angular y el ESP32. También expone
// autenticación JWT, RBAC, notificaciones en tiempo real, Telegram, reportes
// CSV y el agente GESTOCK AI.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/ai"
	"gestock-iot-backend/auth"
	"gestock-iot-backend/broker"
	"gestock-iot-backend/config"
	"gestock-iot-backend/controllers"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/realtime"
	"gestock-iot-backend/reports"
	"gestock-iot-backend/routers"
	"gestock-iot-backend/store"
	"gestock-iot-backend/telegram"
)

func main() {
	cfg := config.Load()

	// Fuente de verdad y servicios de dominio.
	almacen := store.Nuevo()
	hub := realtime.Nuevo(cfg.CorsOrigenes)
	authSvc := auth.Nuevo(cfg)
	notifSvc := notifications.Nuevo(hub)
	tgSvc := telegram.Nuevo(cfg)
	repSvc := reports.Nuevo()
	iaSvc := ai.Nuevo(cfg, almacen, authSvc, repSvc, notifSvc)
	mqttSvc := broker.Nuevo(cfg, almacen)

	// El canal Telegram recibe una copia de las notificaciones autorizadas.
	notifSvc.SetTelegram(tgSvc)

	controllers.Inicializar(almacen, mqttSvc, cfg, authSvc, hub, notifSvc, tgSvc, iaSvc, repSvc)

	// Los eventos MQTT se convierten en WebSocket, notificaciones y auditoría.
	mqttSvc.SetEventos(broker.Eventos{
		Sensor:        controllers.EventoSensor,
		Clasificacion: controllers.EventoClasificacion,
		Conexion:      controllers.EventoMQTT,
	})

	routers.Init()
	tgSvc.Iniciar()
	mqttSvc.Conectar()
	controllers.IniciarVigilanciaESP32()
	controllers.NotificarArranque()

	web.BConfig.AppName = "gestock-iot-backend"
	web.BConfig.RunMode = web.PROD
	web.BConfig.CopyRequestBody = true
	web.BConfig.Listen.HTTPAddr = ""
	web.BConfig.Listen.HTTPPort = cfg.PuertoInt()

	modoIA := "local"
	if cfg.AIEnabled && cfg.AIAPIKey != "" {
		modoIA = "proveedor " + cfg.AIProvider
	}
	log.Printf("GESTOCK backend en http://localhost:%d/api · simulación=%v · IA=%s · Telegram=%v",
		cfg.PuertoInt(), cfg.Simulacion, modoIA, tgSvc.Configurado())
	if cfg.Entorno == "production" && cfg.SeedDemoUsers {
		log.Printf("AVISO: SEED_DEMO_USERS está activo en producción; desactívalo antes de exponer el servicio")
	}

	// Cierre ordenado del cliente MQTT.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("Cerrando…")
		mqttSvc.Desconectar()
		os.Exit(0)
	}()

	web.Run()
}
