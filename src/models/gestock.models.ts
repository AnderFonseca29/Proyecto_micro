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
