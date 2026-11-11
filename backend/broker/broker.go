// Package broker encapsula la integración MQTT (eclipse/paho) del backend:
// se suscribe a los eventos del sensor y del ESP32 y publica las órdenes de
// clasificación. Si no hay broker configurado, funciona en modo simulación.
package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"gestock-iot-backend/config"
	"gestock-iot-backend/store"
)

// Eventos permite que la capa HTTP reaccione a los mensajes del dispositivo
// sin que este paquete dependa de controladores (evita importaciones cíclicas).
type Eventos struct {
	Sensor        func(dispositivo string, presente bool, nueva bool)
	Clasificacion func(rec store.Recepcion, completada bool, detalle string)
	Conexion      func(conectado bool)
}

// Broker gestiona la conexión MQTT y el modo simulación.
type Broker struct {
	cfg       config.Config
	st        *store.Store
	mu        sync.Mutex
	cliente   mqtt.Client
	conectado bool
	simulando bool
	eventos   Eventos
}

// Nuevo crea el servicio de broker (sin conectar todavía).
func Nuevo(cfg config.Config, st *store.Store) *Broker {
	return &Broker{cfg: cfg, st: st}
}

// SetEventos registra los callbacks de la capa de aplicación.
func (b *Broker) SetEventos(e Eventos) {
	b.mu.Lock()
	b.eventos = e
	b.mu.Unlock()
}

// Conectar inicia la conexión MQTT o el modo simulación. No bloquea.
func (b *Broker) Conectar() {
	if b.cfg.MQTTBrokerURL == "" {
		b.iniciarSimulacion()
		return
	}

	opts := mqtt.NewClientOptions().
		AddBroker(b.cfg.MQTTBrokerURL).
		SetClientID(b.cfg.MQTTClientID).
		SetUsername(b.cfg.MQTTUsername).
		SetPassword(b.cfg.MQTTPassword).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOnConnectHandler(b.alConectar).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			anterior := b.st.SetMQTT(false)
			b.setConectado(false)
			log.Printf("[mqtt] conexión perdida: %v", err)
			if anterior {
				b.emitirConexion(false)
			}
		})

	b.mu.Lock()
	b.cliente = mqtt.NewClient(opts)
	b.mu.Unlock()

	log.Printf("[mqtt] conectando a %s…", b.cfg.MQTTBrokerURL)
	tok := b.cliente.Connect()
	if !tok.WaitTimeout(15*time.Second) || tok.Error() != nil {
		log.Printf("[mqtt] no se pudo conectar (se reintentará en segundo plano)")
		b.st.SetMQTT(false)
	}
}

func (b *Broker) alConectar(c mqtt.Client) {
	b.setConectado(true)
	b.st.SetMQTT(true)
	log.Printf("[mqtt] conectado. Suscripción a %s y %s", b.cfg.TopicSensor, b.cfg.TopicEstado)
	c.Subscribe(b.cfg.TopicSensor, 1, b.alSensor)
	c.Subscribe(b.cfg.TopicEstado, 1, b.alEstado)
	b.emitirConexion(true)
}

// Conectado indica si hay conexión activa con el broker.
func (b *Broker) Conectado() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conectado
}

// Simulando indica si el backend está emulando al ESP32.
func (b *Broker) Simulando() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.simulando
}

// PublicarClasificacion envía la orden de clasificación al ESP32.
func (b *Broker) PublicarClasificacion(rec store.Recepcion) error {
	payload, err := json.Marshal(map[string]any{
		"registro":        "exitoso",
		"codigo_producto": rec.CodigoProducto,
		"accion":          "clasificar",
		"id_operacion":    rec.IDOperacion,
	})
	if err != nil {
		return err
	}
	if !b.Conectado() {
		return fmt.Errorf("broker MQTT no disponible")
	}
	b.mu.Lock()
	cliente := b.cliente
	b.mu.Unlock()

	tok := cliente.Publish(b.cfg.TopicClasificador, 1, false, payload)
	if !tok.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("tiempo de espera agotado al publicar")
	}
	if err := tok.Error(); err != nil {
		return err
	}
	log.Printf("[mqtt] orden publicada en %s (op %s)", b.cfg.TopicClasificador, rec.IDOperacion)
	return nil
}

// --- manejadores de mensajes entrantes ---

func (b *Broker) alSensor(_ mqtt.Client, msg mqtt.Message) {
	var ev struct {
		Evento      string `json:"evento"`
		Estado      bool   `json:"estado"`
		Dispositivo string `json:"dispositivo"`
	}
	if err := json.Unmarshal(msg.Payload(), &ev); err != nil {
		log.Printf("[mqtt] sensor: payload inválido: %v", err)
		return
	}
	nueva := b.st.RegistrarDeteccion(ev.Dispositivo, ev.Estado)
	b.st.SetESP32(true, time.Now().UTC().Format(time.RFC3339))
	if nueva {
		log.Printf("[mqtt] caja detectada por %s", ev.Dispositivo)
	}
	b.emitirSensor(ev.Dispositivo, ev.Estado, nueva)
}

func (b *Broker) alEstado(_ mqtt.Client, msg mqtt.Message) {
	var ev struct {
		Estado         string `json:"estado"`
		CodigoProducto string `json:"codigo_producto"`
		IDOperacion    string `json:"id_operacion"`
		Mensaje        string `json:"mensaje"`
	}
	if err := json.Unmarshal(msg.Payload(), &ev); err != nil {
		log.Printf("[mqtt] estado: payload inválido: %v", err)
		return
	}
	b.st.SetESP32(true, time.Now().UTC().Format(time.RFC3339))

	switch ev.Estado {
	case "clasificacion_completada":
		detalle := "Clasificación completada por el ESP32"
		if b.st.MarcarClasificacion(ev.IDOperacion, true, detalle) {
			log.Printf("[mqtt] clasificación completada: %s", ev.IDOperacion)
			b.emitirClasificacion(ev.IDOperacion, true, detalle)
		}
	case "error":
		detalle := "Error del ESP32: " + ev.Mensaje
		if b.st.MarcarClasificacion(ev.IDOperacion, false, detalle) {
			log.Printf("[mqtt] error de clasificación: %s", ev.IDOperacion)
			b.emitirClasificacion(ev.IDOperacion, false, detalle)
		}
	default:
		// Mensajes informativos del dispositivo: se reflejan en el estado IoT.
		b.emitirSensor("", false, false)
	}
}

// --- modo simulación ---

func (b *Broker) iniciarSimulacion() {
	b.mu.Lock()
	b.simulando = true
	b.mu.Unlock()

	b.st.SetSimulacion(true)
	b.st.SetMQTT(false)
	b.st.SetESP32(true, time.Now().UTC().Format(time.RFC3339))
	log.Printf("[mqtt] sin MQTT_BROKER_URL → MODO SIMULACIÓN (ESP32 emulado)")

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			for _, rec := range b.st.Pendientes() {
				time.Sleep(b.cfg.SimulacionDelay)
				detalle := "Clasificación simulada (modo demostración, sin hardware)"
				if b.st.MarcarClasificacion(rec.IDOperacion, true, detalle) {
					b.st.SetESP32(true, time.Now().UTC().Format(time.RFC3339))
					b.emitirClasificacion(rec.IDOperacion, true, detalle)
				}
			}
		}
	}()
}

func (b *Broker) setConectado(v bool) {
	b.mu.Lock()
	b.conectado = v
	b.mu.Unlock()
}

func (b *Broker) emitirConexion(conectado bool) {
	b.mu.Lock()
	fn := b.eventos.Conexion
	b.mu.Unlock()
	if fn != nil {
		fn(conectado)
	}
}

func (b *Broker) emitirSensor(dispositivo string, presente, nueva bool) {
	b.mu.Lock()
	fn := b.eventos.Sensor
	b.mu.Unlock()
	if fn != nil {
		fn(dispositivo, presente, nueva)
	}
}

func (b *Broker) emitirClasificacion(idOperacion string, completada bool, detalle string) {
	b.mu.Lock()
	fn := b.eventos.Clasificacion
	b.mu.Unlock()
	if fn == nil {
		return
	}
	if rec, ok := b.st.RecepcionPorOperacion(idOperacion); ok {
		fn(rec, completada, detalle)
	}
}

// Desconectar cierra el cliente MQTT de forma ordenada.
func (b *Broker) Desconectar() {
	b.mu.Lock()
	cliente := b.cliente
	b.mu.Unlock()
	if cliente != nil && cliente.IsConnected() {
		cliente.Disconnect(250)
	}
}
