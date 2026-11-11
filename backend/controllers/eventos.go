package controllers

import (
	"fmt"
	"log"
	"time"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/store"
)

// EventoRecepcion difunde una recepción registrada y genera los avisos
// internos correspondientes. Nunca confirma la clasificación física: eso solo
// ocurre cuando el ESP32 lo reporta por MQTT.
func EventoRecepcion(rec store.Recepcion, duplicado bool, mqttEnviado, simulado bool) {
	emitirRecepcion(rec)
	emitirEstadoIoT()
	if duplicado || Notificaciones == nil {
		return
	}
	estadoMQTT := "orden enviada al ESP32"
	switch {
	case simulado:
		estadoMQTT = "modo simulación: el ESP32 emulado confirmará la clasificación"
	case !mqttEnviado:
		estadoMQTT = "orden MQTT pendiente; puede reintentarse"
	}
	Notificaciones.Crear(notifications.Entrada{
		Categoria: notifications.CatRecepcion,
		Prioridad: notifications.PrioridadMedia,
		Titulo:    "Recepción registrada",
		Mensaje: fmt.Sprintf("%d uds de %s (%s) ingresaron al inventario. Operación %s · %s. La clasificación física está %s.",
			rec.Cantidad, rec.NombreProducto, rec.CodigoProducto, rec.IDOperacion, estadoMQTT, rec.Clasificacion),
		Modulo:  "/recepcion",
		Entidad: rec.IDOperacion,
		Roles:   []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolOperario)},
		Clave:   "recepcion-" + rec.IDOperacion,
	})
}

// EventoSensor difunde la lectura del HC-SR04. No crea notificaciones para no
// saturar el centro con cada detección.
func EventoSensor(dispositivo string, presente bool, nueva bool) {
	if Hub == nil || Almacen == nil {
		return
	}
	Hub.Emitir("iot_sensor", map[string]any{
		"dispositivo": dispositivo,
		"caja_presente": presente,
		"nueva":         nueva,
	}, puedeVerIoT)
	emitirEstadoIoT()
}

// EventoClasificacion se invoca cuando el ESP32 confirma o falla el movimiento
// físico. Solo una confirmación del dispositivo cambia el estado.
func EventoClasificacion(rec store.Recepcion, completada bool, detalle string) {
	emitirRecepcion(rec)
	emitirEstadoIoT()
	if Notificaciones == nil {
		return
	}
	if completada {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatRecepcion,
			Prioridad: notifications.PrioridadBaja,
			Titulo:    "Clasificación física completada",
			Mensaje: fmt.Sprintf("El ESP32 confirmó la clasificación de %s (%s), operación %s. %s",
				rec.NombreProducto, rec.CodigoProducto, rec.IDOperacion, valorTexto(detalle, "")),
			Modulo:  "/recepcion",
			Entidad: rec.IDOperacion,
			Roles:   []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolOperario)},
			Clave:   "clasificacion-ok-" + rec.IDOperacion,
		})
		return
	}
	Notificaciones.Crear(notifications.Entrada{
		Categoria: notifications.CatIoT,
		Prioridad: notifications.PrioridadAlta,
		Titulo:    "Clasificación física fallida",
		Mensaje: fmt.Sprintf("El ESP32 no pudo clasificar %s (%s), operación %s. %s Revisa el servo, el sensor y la alimentación antes de reintentar.",
			rec.NombreProducto, rec.CodigoProducto, rec.IDOperacion, valorTexto(detalle, "")),
		Modulo:  "/mantenimiento",
		Entidad: rec.IDOperacion,
		Roles:   []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)},
		Clave:   "clasificacion-fallida-" + rec.IDOperacion,
	})
}

// EventoMQTT refleja las transiciones de conexión del broker.
func EventoMQTT(conectado bool) {
	emitirEstadoIoT()
	if Almacen != nil {
		evento := "MQTT_DESCONECTADO"
		if conectado {
			evento = "MQTT_CONECTADO"
		}
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: evento, Detalle: "Cambio de estado de la conexión MQTT",
			Usuario: "sistema", Rol: "SISTEMA", Severidad: "INFO",
		})
	}
	if Notificaciones == nil {
		return
	}
	if !conectado {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatIoT,
			Prioridad: notifications.PrioridadAlta,
			Titulo:    "Conexión MQTT perdida",
			Mensaje:   "El backend perdió la conexión con el broker MQTT. El inventario sigue operativo, pero las órdenes al ESP32 quedan pendientes hasta reconectar.",
			Modulo:    "/configuracion",
			Roles:     []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)},
			Clave:     "mqtt-desconectado",
		})
		return
	}
	Notificaciones.Crear(notifications.Entrada{
		Categoria: notifications.CatIoT,
		Prioridad: notifications.PrioridadMedia,
		Titulo:    "Conexión MQTT restablecida",
		Mensaje:   "El backend volvió a conectarse con el broker MQTT. Las órdenes pendientes pueden reintentarse.",
		Modulo:    "/configuracion",
		Roles:     []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)},
		Clave:     "mqtt-conectado",
	})
}

// EventoESP32 registra y difunde la presencia del microcontrolador.
func EventoESP32(online bool) {
	emitirEstadoIoT()
	if Almacen != nil {
		evento := "ESP32_DESCONECTADO"
		if online {
			evento = "ESP32_CONECTADO"
		}
		Almacen.RegistrarAuditoria(store.Auditoria{
			Evento: evento, Detalle: "Cambio de presencia del ESP32",
			Usuario: "sistema", Rol: "SISTEMA", Severidad: "INFO",
		})
	}
	if !online && Notificaciones != nil {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatIoT,
			Prioridad: notifications.PrioridadAlta,
			Titulo:    "ESP32 sin comunicación",
			Mensaje:   "No se reciben mensajes del ESP32 dentro del tiempo esperado. Esto no confirma una avería: revisa alimentación, Wi-Fi y credenciales MQTT. La recepción de inventario no se bloquea.",
			Modulo:    "/mantenimiento",
			Roles:     []string{string(auth.RolAdmin), string(auth.RolTecnico), string(auth.RolSupervisor)},
			Clave:     "esp32-desconectado",
		})
	}
}

// IniciarVigilanciaESP32 marca el dispositivo como desconectado cuando deja de
// enviar mensajes. En modo simulación no aplica porque no hay hardware real.
func IniciarVigilanciaESP32() {
	if Cfg.ESP32Timeout <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for range t.C {
			if MQTT != nil && MQTT.Simulando() {
				continue
			}
			online, ultima := Almacen.EstadoESP32()
			if !online {
				continue
			}
			visto, err := time.Parse(time.RFC3339, ultima)
			if err != nil || time.Since(visto) <= Cfg.ESP32Timeout {
				continue
			}
			if Almacen.SetESP32(false, "") {
				log.Printf("[iot] ESP32 sin comunicación desde %s", ultima)
				EventoESP32(false)
			}
		}
	}()
}

// NotificarArranque deja evidencia visible de que el backend está listo y, en
// desarrollo, recuerda que las cuentas demo deben desactivarse en producción.
func NotificarArranque() {
	if Notificaciones == nil {
		return
	}
	Notificaciones.Crear(notifications.Entrada{
		Categoria: notifications.CatSistema,
		Prioridad: notifications.PrioridadBaja,
		Titulo:    "GESTOCK iniciado",
		Mensaje:   "El backend está en línea. Inventario, notificaciones, reportes, Telegram y GESTOCK AI están disponibles según los permisos de cada rol.",
		Modulo:    "/",
		Roles:     []string{string(auth.RolAdmin)},
		Clave:     "arranque-backend",
	})
	if Cfg.SeedDemoUsers && Cfg.Entorno != "production" {
		Notificaciones.Crear(notifications.Entrada{
			Categoria: notifications.CatSeguridad,
			Prioridad: notifications.PrioridadMedia,
			Titulo:    "Cuentas de demostración activas",
			Mensaje:   "El entorno de desarrollo tiene usuarios demo sembrados. Antes de desplegar, define SEED_DEMO_USERS=false y contraseñas propias; no compartas credenciales por la interfaz.",
			Modulo:    "/configuracion",
			Roles:     []string{string(auth.RolAdmin), string(auth.RolAuditor)},
			Clave:     "demo-users-activos",
		})
	}
}
