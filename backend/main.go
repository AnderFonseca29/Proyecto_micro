// GESTOCK · Recepción Inteligente IoT — backend Go/Beego.
//
// Servicio independiente que actúa como fuente de verdad de los datos
// temporales de la demostración (catálogo, stock e historial en memoria) y
// como puente MQTT entre la interfaz Angular y el ESP32.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/beego/beego/v2/server/web"

	"gestock-iot-backend/broker"
	"gestock-iot-backend/config"
	"gestock-iot-backend/controllers"
	"gestock-iot-backend/routers"
	"gestock-iot-backend/store"
)

func main() {
	cfg := config.Load()

	almacen := store.Nuevo()
	mqttSvc := broker.Nuevo(cfg, almacen)

	controllers.Almacen = almacen
	controllers.MQTT = mqttSvc
	controllers.Cfg = cfg

	routers.Init()
	mqttSvc.Conectar()

	web.BConfig.AppName = "gestock-iot-backend"
	web.BConfig.RunMode = web.PROD
	web.BConfig.CopyRequestBody = true
	web.BConfig.Listen.HTTPAddr = ""
	web.BConfig.Listen.HTTPPort = cfg.PuertoInt()

	log.Printf("GESTOCK IoT backend escuchando en http://localhost:%d/api (simulación=%v)",
		cfg.PuertoInt(), cfg.Simulacion)

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
