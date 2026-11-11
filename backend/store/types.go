package store

// Producto del catálogo (vive en memoria RAM en esta versión de demostración).
type Producto struct {
	Codigo       string `json:"codigo"`
	Nombre       string `json:"nombre"`
	Categoria    string `json:"categoria"`
	Stock        int    `json:"stock"`
	StockInicial int    `json:"stock_inicial"`
	StockMinimo  int    `json:"stock_minimo"`
	Activo       bool   `json:"activo"`
	ActualizadoEn string `json:"actualizado_en"`
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

// Tipos de movimiento de inventario.
const (
	MovEntrada = "ENTRADA"
	MovSalida  = "SALIDA"
	MovAjuste  = "AJUSTE"
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
	Usuario            string `json:"usuario,omitempty"`
	FechaRegistro      string `json:"fecha_registro"`
	FechaClasificacion string `json:"fecha_clasificacion,omitempty"`
}

// Movimiento de inventario (entrada, salida o ajuste). Es la fuente de verdad
// para auditoría, reportes y herramientas del agente IA.
type Movimiento struct {
	ID             int64  `json:"id"`
	Tipo           string `json:"tipo"`
	CodigoProducto string `json:"codigo_producto"`
	NombreProducto string `json:"nombre_producto"`
	Cantidad       int    `json:"cantidad"`
	StockAnterior  int    `json:"stock_anterior"`
	StockNuevo     int    `json:"stock_nuevo"`
	Motivo         string `json:"motivo"`
	Usuario        string `json:"usuario"`
	Rol            string `json:"rol"`
	FechaRegistro  string `json:"fecha_registro"`
}

// Incidencia de mantenimiento / IoT.
type Incidencia struct {
	ID            int64  `json:"id"`
	Titulo        string `json:"titulo"`
	Descripcion   string `json:"descripcion"`
	Prioridad     string `json:"prioridad"` // BAJA | MEDIA | ALTA | CRITICA
	Estado        string `json:"estado"`    // ABIERTA | EN_PROCESO | RESUELTA
	Dispositivo   string `json:"dispositivo"`
	CreadaPor     string `json:"creada_por"`
	AsignadaA     string `json:"asignada_a,omitempty"`
	Resolucion    string `json:"resolucion,omitempty"`
	FechaCreacion string `json:"fecha_creacion"`
	FechaCierre   string `json:"fecha_cierre,omitempty"`
}

// Auditoria es un evento de seguridad o cambio importante.
type Auditoria struct {
	ID        int64  `json:"id"`
	Evento    string `json:"evento"`
	Detalle   string `json:"detalle"`
	Usuario   string `json:"usuario"`
	Rol       string `json:"rol"`
	IP        string `json:"ip,omitempty"`
	Severidad string `json:"severidad"` // INFO | MEDIA | ALTA | CRITICA
	Fecha     string `json:"fecha"`
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

// ResumenInventario agrega los indicadores que consume el panel y la IA.
type ResumenInventario struct {
	TotalProductos      int `json:"total_productos"`
	ProductosActivos    int `json:"productos_activos"`
	ProductosStockBajo  int `json:"productos_stock_bajo"`
	ProductosAgotados   int `json:"productos_agotados"`
	UnidadesDisponibles int `json:"unidades_disponibles"`
	EntradasHoy         int `json:"entradas_hoy"`
	SalidasHoy          int `json:"salidas_hoy"`
	MovimientosHoy      int `json:"movimientos_hoy"`
	RecepcionesHoy      int `json:"recepciones_hoy"`
	IncidenciasAbiertas int `json:"incidencias_abiertas"`
}

// SolicitudRecepcion es el cuerpo de POST /api/demo/recepciones.
type SolicitudRecepcion struct {
	CodigoProducto string `json:"codigo_producto"`
	Cantidad       int    `json:"cantidad"`
	IDOperacion    string `json:"id_operacion"`
	Origen         string `json:"origen"`
	Usuario        string `json:"-"`
	Rol            string `json:"-"`
}

// SolicitudMovimiento es el cuerpo de salidas/ajustes de inventario.
type SolicitudMovimiento struct {
	CodigoProducto string `json:"codigo_producto"`
	Cantidad       int    `json:"cantidad"`
	StockNuevo     *int   `json:"stock_nuevo,omitempty"`
	Motivo         string `json:"motivo"`
	Usuario        string `json:"-"`
	Rol            string `json:"-"`
}

// SolicitudIncidencia es el cuerpo de POST /api/mantenimiento/incidencias.
type SolicitudIncidencia struct {
	Titulo      string `json:"titulo"`
	Descripcion string `json:"descripcion"`
	Prioridad   string `json:"prioridad"`
	Dispositivo string `json:"dispositivo"`
	AsignadaA   string `json:"asignada_a"`
	CreadaPor   string `json:"-"`
}

// CambioIncidencia actualiza el estado de una incidencia.
type CambioIncidencia struct {
	Estado     string `json:"estado"`
	Resolucion string `json:"resolucion"`
	AsignadaA  string `json:"asignada_a"`
}
