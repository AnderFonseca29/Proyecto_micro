package store

// Producto del catálogo temporal de demostración (vive en memoria RAM).
type Producto struct {
	Codigo       string `json:"codigo"`
	Nombre       string `json:"nombre"`
	Stock        int    `json:"stock"`
	StockInicial int    `json:"stock_inicial"`
	Activo       bool   `json:"activo"`
}

// Estados de registro de una recepción.
const (
	EstadoRegistrado  = "REGISTRADO"
	EstadoDesconocido = "DESCONOCIDO"
	EstadoError       = "ERROR"
)

// Estados de clasificación física (confirmados por el ESP32 vía MQTT).
const (
	ClasificacionPendiente  = "PENDIENTE"
	ClasificacionCompletada = "COMPLETADA"
	ClasificacionFallida    = "FALLIDA"
)

// Recepcion es un ingreso registrado en el historial temporal.
type Recepcion struct {
	ID                 int64  `json:"id"`
	CodigoProducto     string `json:"codigo_producto"`
	NombreProducto     string `json:"nombre_producto"`
	Cantidad           int    `json:"cantidad"`
	IDOperacion        string `json:"id_operacion"`
	Estado             string `json:"estado"`
	Clasificacion      string `json:"clasificacion"`
	Origen             string `json:"origen"`
	FechaRegistro      string `json:"fecha_registro"`
	FechaClasificacion string `json:"fecha_clasificacion,omitempty"`
}

// SensorEstado refleja el HC-SR04 de la maqueta.
type SensorEstado struct {
	CajaPresente    bool   `json:"caja_presente"`
	UltimaDeteccion string `json:"ultima_deteccion,omitempty"`
	Dispositivo     string `json:"dispositivo"`
	Detecciones     int    `json:"detecciones"`
}

// EstadoIoT es el resumen que consume la interfaz (indicadores + panel maqueta).
type EstadoIoT struct {
	Sensor           SensorEstado `json:"sensor"`
	ESP32Online      bool         `json:"esp32_online"`
	ESP32LastSeen    string       `json:"esp32_last_seen,omitempty"`
	MQTTConectado    bool         `json:"mqtt_conectado"`
	UltimoMensaje    string       `json:"ultimo_mensaje"`
	Servo            string       `json:"servo"`
	Clasificacion    string       `json:"clasificacion"`
	Simulacion       bool         `json:"simulacion"`
	TotalProductos   int          `json:"total_productos"`
	TotalRecepciones int          `json:"total_recepciones"`
	Registrados      int          `json:"registrados"`
	Desconocidos     int          `json:"desconocidos"`
	UnidadesHoy      int          `json:"unidades_hoy"`
}

// SolicitudRecepcion es el cuerpo de POST /api/demo/recepciones.
type SolicitudRecepcion struct {
	CodigoProducto string `json:"codigo_producto"`
	Cantidad       int    `json:"cantidad"`
	IDOperacion    string `json:"id_operacion"`
	Origen         string `json:"origen"`
}
