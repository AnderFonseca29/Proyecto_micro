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

// Config agrupa toda la configuración del servicio.
type Config struct {
	Puerto       string
	CorsOrigenes []string
	Entorno      string

	// Autenticación / seguridad.
	JWTSecret        string
	JWTTTL           time.Duration
	SeedDemoUsers    bool
	AdminPassword    string
	SupervisorPass   string
	OperarioPass     string
	TecnicoPass      string
	AuditorPass      string
	LoginMaxIntentos int

	// MQTT / EMQX Cloud.
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
	ESP32Timeout    time.Duration

	// Notificaciones / WebSocket.
	WebSocketEnabled bool
	NotificationTTL  time.Duration

	// Telegram.
	TelegramEnabled    bool
	TelegramBotToken   string
	TelegramBotUser    string
	TelegramWebhookSec string
	TelegramWebhookURL string
	TelegramGroupID    string

	// Inteligencia artificial (proveedor compatible con OpenAI).
	AIEnabled      bool
	AILocalFallback bool
	AIProvider     string
	AIBaseURL      string
	AIAPIKey       string
	AIModel        string
	AIMaxTokens    int
	AITimeout      time.Duration
	AIMaxRondas    int
	AIRateLimit    int
	AIHistorial    int
	AITemperatura  float64
}

// Load lee .env (si existe) y construye la configuración con valores por
// defecto seguros. Las variables ya presentes en el entorno tienen prioridad.
func Load() Config {
	cargarEnv(".env")

	cfg := Config{
		Puerto:        obtener("PORT", "8080"),
		CorsOrigenes:  separar(obtener("CORS_ORIGINS", "http://localhost:4200,http://localhost:4300")),
		Entorno:       obtener("APP_ENV", "development"),
		JWTSecret:     obtener("JWT_SECRET", ""),
		JWTTTL:        time.Duration(obtenerInt("JWT_TTL_MINUTES", 480)) * time.Minute,
		SeedDemoUsers: obtenerBool("SEED_DEMO_USERS", true),

		AdminPassword:    obtener("SEED_ADMIN_PASSWORD", "Admin123!"),
		SupervisorPass:   obtener("SEED_SUPERVISOR_PASSWORD", "Super123!"),
		OperarioPass:     obtener("SEED_OPERARIO_PASSWORD", "Oper123!"),
		TecnicoPass:      obtener("SEED_TECNICO_PASSWORD", "Tecnico123!"),
		AuditorPass:      obtener("SEED_AUDITOR_PASSWORD", "Auditor123!"),
		LoginMaxIntentos: obtenerInt("LOGIN_MAX_INTENTOS", 5),

		MQTTBrokerURL:     obtener("MQTT_BROKER_URL", ""),
		MQTTUsername:      obtener("MQTT_USERNAME", ""),
		MQTTPassword:      obtener("MQTT_PASSWORD", ""),
		MQTTClientID:      obtener("MQTT_CLIENT_ID", "gestock_backend"),
		TopicSensor:       obtener("MQTT_TOPIC_SENSOR", "gestock/recepcion/sensor"),
		TopicClasificador: obtener("MQTT_TOPIC_CLASIFICADOR", "gestock/recepcion/clasificador"),
		TopicEstado:       obtener("MQTT_TOPIC_ESTADO", "gestock/recepcion/estado"),
		ESP32Timeout:      time.Duration(obtenerInt("ESP32_TIMEOUT_SECONDS", 45)) * time.Second,

		WebSocketEnabled: obtenerBool("WEBSOCKET_ENABLED", true),
		NotificationTTL:  time.Duration(obtenerInt("NOTIFICATION_RETENTION_DAYS", 30)) * 24 * time.Hour,

		TelegramEnabled:    obtenerBool("TELEGRAM_ENABLED", false),
		TelegramBotToken:   obtener("TELEGRAM_BOT_TOKEN", ""),
		TelegramBotUser:    obtener("TELEGRAM_BOT_USERNAME", ""),
		TelegramWebhookSec: obtener("TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramWebhookURL: obtener("TELEGRAM_WEBHOOK_URL", ""),
		TelegramGroupID:    obtener("TELEGRAM_GROUP_CHAT_ID", ""),

		AIEnabled:       obtenerBool("AI_ENABLED", true),
		AILocalFallback: obtenerBool("AI_LOCAL_FALLBACK", true),
		AIProvider:      obtener("AI_PROVIDER", "openai"),
		AIBaseURL:       obtener("AI_BASE_URL", ""),
		AIAPIKey:        obtener("AI_API_KEY", ""),
		AIModel:         obtener("AI_MODEL", "gpt-4o-mini"),
		AIMaxTokens:     obtenerInt("AI_MAX_TOKENS", 900),
		AITimeout:       time.Duration(obtenerInt("AI_REQUEST_TIMEOUT", 30)) * time.Second,
		AIMaxRondas:     obtenerInt("AI_MAX_TOOL_ROUNDS", 4),
		AIRateLimit:     obtenerInt("AI_RATE_LIMIT_PER_MINUTE", 20),
		AIHistorial:     obtenerInt("AI_MAX_HISTORY", 10),
		AITemperatura:   obtenerFloat("AI_TEMPERATURE", 0.2),
	}

	cfg.Simulacion = cfg.MQTTBrokerURL == ""
	cfg.SimulacionDelay = time.Duration(obtenerInt("MQTT_SIMULACION_DELAY_MS", 1200)) * time.Millisecond

	// URL base por defecto según el proveedor compatible con OpenAI. Se puede
	// sobrescribir con AI_BASE_URL sin cambiar el código.
	if cfg.AIBaseURL == "" {
		switch strings.ToLower(cfg.AIProvider) {
		case "openrouter":
			cfg.AIBaseURL = "https://openrouter.ai/api/v1"
		case "groq":
			cfg.AIBaseURL = "https://api.groq.com/openai/v1"
		case "deepseek":
			cfg.AIBaseURL = "https://api.deepseek.com/v1"
		case "ollama":
			cfg.AIBaseURL = "http://localhost:11434/v1"
		default:
			cfg.AIBaseURL = "https://api.openai.com/v1"
		}
	}
	if cfg.AIMaxRondas <= 0 {
		cfg.AIMaxRondas = 4
	}
	if cfg.AIRateLimit <= 0 {
		cfg.AIRateLimit = 20
	}
	if cfg.AIHistorial <= 0 {
		cfg.AIHistorial = 10
	}
	if cfg.AITimeout <= 0 {
		cfg.AITimeout = 30 * time.Second
	}

	if cfg.JWTSecret == "" {
		// Secreto efímero: invalida sesiones al reiniciar. En producción es
		// OBLIGATORIO definir JWT_SECRET.
		cfg.JWTSecret = "gestock-dev-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if cfg.TelegramBotToken == "" {
		cfg.TelegramEnabled = false
	}
	// Sin AI_API_KEY no se llama a un proveedor externo, pero el analizador
	// local puede seguir respondiendo con datos reales si AI_LOCAL_FALLBACK=true.
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

func obtenerFloat(clave string, porDefecto float64) float64 {
	n, err := strconv.ParseFloat(obtener(clave, "0"), 64)
	if err != nil {
		return porDefecto
	}
	return n
}

func obtenerBool(clave string, porDefecto bool) bool {
	v := strings.ToLower(obtener(clave, strconv.FormatBool(porDefecto)))
	return v == "1" || v == "true" || v == "yes" || v == "si" || v == "sí"
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
