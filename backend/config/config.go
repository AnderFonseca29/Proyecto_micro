// Package config carga la configuración del backend desde variables de
// entorno (con soporte para un archivo .env local, que nunca se versiona).
package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config agrupa toda la configuración del servicio IoT.
type Config struct {
	Puerto            string
	CorsOrigenes      []string
	MQTTBrokerURL     string
	MQTTUsername      string
	MQTTPassword      string
	MQTTClientID      string
	TopicSensor       string
	TopicClasificador string
	TopicEstado       string
	// Simulacion se activa cuando no hay broker configurado: el backend
	// emula al ESP32 para poder demostrar el flujo sin hardware.
	Simulacion      bool
	SimulacionDelay time.Duration
}

// Load lee .env (si existe) y construye la configuración con valores por
// defecto seguros. Las variables ya presentes en el entorno tienen prioridad.
func Load() Config {
	cargarEnv(".env")

	cfg := Config{
		Puerto:            obtener("PORT", "8080"),
		CorsOrigenes:      separar(obtener("CORS_ORIGINS", "*")),
		MQTTBrokerURL:     obtener("MQTT_BROKER_URL", ""),
		MQTTUsername:      obtener("MQTT_USERNAME", ""),
		MQTTPassword:      obtener("MQTT_PASSWORD", ""),
		MQTTClientID:      obtener("MQTT_CLIENT_ID", "gestock_backend"),
		TopicSensor:       obtener("MQTT_TOPIC_SENSOR", "gestock/recepcion/sensor"),
		TopicClasificador: obtener("MQTT_TOPIC_CLASIFICADOR", "gestock/recepcion/clasificador"),
		TopicEstado:       obtener("MQTT_TOPIC_ESTADO", "gestock/recepcion/estado"),
	}

	cfg.Simulacion = cfg.MQTTBrokerURL == ""
	cfg.SimulacionDelay = time.Duration(obtenerInt("MQTT_SIMULACION_DELAY_MS", 1200)) * time.Millisecond
	return cfg
}

// PuertoInt devuelve el puerto como entero (8080 si el valor no es válido).
func (c Config) PuertoInt() int {
	n, err := strconv.Atoi(strings.TrimSpace(c.Puerto))
	if err != nil || n <= 0 {
		return 8080
	}
	return n
}

// --- utilidades internas ---

func cargarEnv(ruta string) {
	f, err := os.Open(ruta)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		linea := strings.TrimSpace(sc.Text())
		if linea == "" || strings.HasPrefix(linea, "#") {
			continue
		}
		partes := strings.SplitN(linea, "=", 2)
		if len(partes) != 2 {
			continue
		}
		clave := strings.TrimSpace(partes[0])
		valor := strings.Trim(strings.TrimSpace(partes[1]), `"'`)
		if _, existe := os.LookupEnv(clave); !existe {
			_ = os.Setenv(clave, valor)
		}
	}
}

func obtener(clave, porDefecto string) string {
	if v, ok := os.LookupEnv(clave); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return porDefecto
}

func obtenerInt(clave string, porDefecto int) int {
	n, err := strconv.Atoi(obtener(clave, strconv.Itoa(porDefecto)))
	if err != nil {
		return porDefecto
	}
	return n
}

func separar(valor string) []string {
	partes := strings.Split(valor, ",")
	salida := make([]string, 0, len(partes))
	for _, p := range partes {
		if t := strings.TrimSpace(p); t != "" {
			salida = append(salida, t)
		}
	}
	return salida
}
