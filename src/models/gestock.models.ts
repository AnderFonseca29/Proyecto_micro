/**
 * GESTOCK (SIGAD) — Modelos compartidos del frontend.
 * Espejo exacto del contrato del backend (JSON).
 */

/** Códigos de stand del almacén. */
export type StandCodigo = 'A' | 'B' | 'C';

/** Tamaño de caja detectado: Pequeña, Mediana, Grande. */
export type Talla = 'P' | 'M' | 'G';

/** Caja detectada por el ESP32 que espera autorización desde la web. */
export interface Deteccion {
  id: string;
  talla: Talla;
  stand: StandCodigo;   // stand sugerido según el tamaño
  recibida: number;     // Date.now()
}

/** Origen del movimiento. */
export type Origen = 'hardware' | 'web';

/** Estado de la conexión WebSocket en tiempo real. */
export type EstadoConexion = 'conectando' | 'en_linea' | 'reconectando' | 'desconectado';

/** Stand del almacén con ocupación en vivo (GET /api/stands). */
export interface Stand {
  codigo: StandCodigo;
  nombre: string;
  capacidad: number;
  ocupacion: number;
  actualizado_en: string;
}

/** Movimiento de auditoría (tabla movimientos). */
export interface Movimiento {
  id: number;
  rfid: string;
  stand: StandCodigo;
  estado: string;          // CLASIFICADO | STAND_LLENO | RECHAZADO
  talla?: Talla;           // P | M | G (tamaño detectado por el ESP32)
  origen: Origen;
  device_id: string;
  fecha_registro: string;  // ISO-8601
}

/** Estado del ESP32 (GET /api/dispositivo y WS device_status). */
export interface DeviceStatus {
  device_id: string;
  online: boolean;
  last_seen: string;
  ip?: string;
  fw?: string;
  uptime_s?: number;
  mqtt_conectado?: boolean;
}

/** Respuesta paginada estándar. */
export interface PageResult<T> {
  data: T[];
  total: number;
  page: number;
  limit: number;
}

/** Filtros de la tabla de auditoría (query params del backend). */
export interface MovimientoFilters {
  stand: string;    // '' | 'A' | 'B' | 'C'
  estado: string;   // '' | 'CLASIFICADO' | 'STAND_LLENO' | …
  origen: string;   // '' | 'hardware' | 'web'
  q: string;        // búsqueda por UID
  desde: string;    // yyyy-MM-dd
  hasta: string;    // yyyy-MM-dd
  page: number;
  limit: number;
}

/** Sobre tipado de los mensajes WebSocket. */
export interface WsMessage<T = unknown> {
  type: 'movimiento' | 'movimientos' | 'stands' | 'device_status' | 'ping';
  data: T;
}

/** Respuesta de POST /api/clasificar y /api/comandos. */
export interface ComandoResponse {
  cmd_id: string;
  stand?: string;
  rfid?: string;
  cmd?: string;
  estado?: string;
}

/** GET /api/health. */
export interface HealthStatus {
  status: string;
  db: boolean;
  mqtt: boolean;
  ts: number;
}

/** GET /api/kpis. */
export interface Kpis {
  total_hoy: number;
  ultimo: Movimiento | null;
}

// ============================================================
//  Recepción Inteligente IoT (cámara → backend Go → MQTT → ESP32)
// ============================================================

/** Origen del ingreso. */
export type OrigenRecepcion = 'camara' | 'manual' | 'sensor' | 'web';

/** Resultado de validar y registrar un código escaneado. */
export type EstadoRecepcion = 'REGISTRADO' | 'DESCONOCIDO' | 'ERROR';

/** Estado de la clasificación física confirmada por el ESP32. */
export type EstadoClasificacion = 'PENDIENTE' | 'COMPLETADA' | 'FALLIDA';

/** Producto del catálogo temporal (memoria del backend / respaldo local). */
export interface Producto {
  codigo: string;
  nombre: string;
  stock: number;
  stock_inicial: number;
  activo: boolean;
}

/** Ingreso registrado al escanear un código de barras / QR. */
export interface Recepcion {
  id: number;
  codigo_producto: string;
  nombre_producto: string;
  cantidad: number;
  id_operacion: string;
  estado: EstadoRecepcion;
  /** null cuando el registro no llegó a clasificarse (p. ej. desconocido). */
  clasificacion: EstadoClasificacion | null;
  origen: OrigenRecepcion;
  fecha_registro: string; // ISO-8601
  fecha_clasificacion?: string;
}

/** Estado del sensor HC-SR04. */
export interface SensorEstado {
  caja_presente: boolean;
  ultima_deteccion?: string;
  dispositivo: string;
  detecciones: number;
}

/** Estado IoT consolidado (GET /api/demo/estado-iot). */
export interface EstadoIoT {
  sensor: SensorEstado;
  esp32_online: boolean;
  esp32_last_seen?: string;
  mqtt_conectado: boolean;
  ultimo_mensaje: string;
  servo: 'REPOSO' | 'CLASIFICANDO';
  clasificacion: string;
  simulacion: boolean;
  total_productos: number;
  total_recepciones: number;
  registrados: number;
  desconocidos: number;
  unidades_hoy: number;
}

/** Respuesta de POST /api/demo/recepciones. */
export interface RespuestaRecepcion {
  ok: boolean;
  duplicado?: boolean;
  recepcion?: Recepcion;
  producto?: Producto;
  stock?: number;
  mqtt_enviado?: boolean;
  clasificacion?: EstadoClasificacion;
  estado?: EstadoRecepcion;
  codigo?: string;
  mensaje?: string;
}

/** Resultado ya normalizado que consume la interfaz. */
export interface ResultadoRecepcion {
  ok: boolean;
  estado: EstadoRecepcion;
  codigo: string;
  producto?: string;
  stock?: number;
  stock_inicial?: number;
  cantidad: number;
  mensaje: string;
  mqtt_enviado: boolean;
  /** null cuando el ingreso no se registró (no aplica clasificación). */
  clasificacion: EstadoClasificacion | null;
  id_operacion: string;
  fecha_registro: string;
  origen: OrigenRecepcion;
  duplicado?: boolean;
}

/** Evento del sensor HC-SR04 (topic gestock/recepcion/sensor). */
export interface EventoSensor {
  evento: string;
  estado: boolean;
  dispositivo: string;
}

/** Confirmación de clasificación del ESP32 (topic gestock/recepcion/estado). */
export interface EventoClasificacion {
  estado: string;
  codigo_producto?: string;
  id_operacion?: string;
  mensaje?: string;
}

