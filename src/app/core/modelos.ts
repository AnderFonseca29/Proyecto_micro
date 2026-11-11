/** Contratos de la API GESTOCK (backend Go/Beego). */

export type Rol = 'ADMINISTRADOR' | 'SUPERVISOR' | 'OPERARIO' | 'TECNICO' | 'AUDITOR';

export interface Usuario {
  id: string;
  nombre: string;
  email: string;
  usuario: string;
  rol: Rol;
  activo: boolean;
  permisos: string[];
  creado_en: string;
}

export interface RespuestaLogin {
  ok: boolean;
  token: string;
  expira: string;
  usuario: Usuario;
}

export interface Producto {
  codigo: string;
  nombre: string;
  categoria: string;
  stock: number;
  stock_inicial: number;
  stock_minimo: number;
  activo: boolean;
  actualizado_en: string;
}

export interface ResumenInventario {
  total_productos: number;
  productos_activos: number;
  productos_stock_bajo: number;
  productos_agotados: number;
  unidades_disponibles: number;
  entradas_hoy: number;
  salidas_hoy: number;
  movimientos_hoy: number;
  recepciones_hoy: number;
  incidencias_abiertas: number;
}

export type TipoMovimiento = 'ENTRADA' | 'SALIDA' | 'AJUSTE';

export interface Movimiento {
  id: number;
  tipo: TipoMovimiento;
  codigo_producto: string;
  nombre_producto: string;
  cantidad: number;
  stock_anterior: number;
  stock_nuevo: number;
  motivo: string;
  usuario: string;
  rol: string;
  fecha_registro: string;
}

export type EstadoRecepcion = 'REGISTRADO' | 'DESCONOCIDO' | 'ERROR';
export type EstadoClasificacion = 'PENDIENTE' | 'COMPLETADA' | 'FALLIDA';

export interface Recepcion {
  id: number;
  codigo_producto: string;
  nombre_producto: string;
  cantidad: number;
  id_operacion: string;
  estado: EstadoRecepcion;
  clasificacion: EstadoClasificacion;
  origen: string;
  usuario?: string;
  fecha_registro: string;
  fecha_clasificacion?: string;
}

export interface Incidencia {
  id: number;
  titulo: string;
  descripcion: string;
  prioridad: 'BAJA' | 'MEDIA' | 'ALTA' | 'CRITICA';
  estado: 'ABIERTA' | 'EN_PROCESO' | 'RESUELTA';
  dispositivo: string;
  creada_por: string;
  asignada_a?: string;
  resolucion?: string;
  fecha_creacion: string;
  fecha_cierre?: string;
}

export interface Auditoria {
  id: number;
  evento: string;
  detalle: string;
  usuario: string;
  rol: string;
  ip?: string;
  severidad: 'INFO' | 'MEDIA' | 'ALTA' | 'CRITICA';
  fecha: string;
}

export interface SensorEstado {
  caja_presente: boolean;
  ultima_deteccion?: string;
  dispositivo: string;
  detecciones: number;
}

export interface EstadoIoT {
  sensor: SensorEstado;
  esp32_online: boolean;
  esp32_last_seen?: string;
  mqtt_conectado: boolean;
  ultimo_mensaje: string;
  servo: string;
  clasificacion: string;
  simulacion: boolean;
  total_productos: number;
  total_recepciones: number;
  registrados: number;
  desconocidos: number;
  unidades_hoy: number;
}

export type CategoriaNotificacion =
  | 'INVENTARIO'
  | 'RECEPCION'
  | 'IOT'
  | 'MANTENIMIENTO'
  | 'SEGURIDAD'
  | 'AUDITORIA'
  | 'IA'
  | 'SISTEMA';

export type Prioridad = 'BAJA' | 'MEDIA' | 'ALTA' | 'CRITICA';

export interface Notificacion {
  id: string;
  categoria: CategoriaNotificacion;
  prioridad: Prioridad;
  titulo: string;
  mensaje: string;
  modulo: string;
  entidad?: string;
  metadata?: Record<string, string>;
  roles: string[];
  usuario_id?: string;
  creada_en: string;
  leida: boolean;
  archivada: boolean;
}

export interface EstadoIA {
  disponible: boolean;
  modo: 'proveedor' | 'local' | 'desactivado';
  proveedor: string;
  modelo: string;
  limite_por_minuto: number;
  sugerencias: string[];
}

export interface MensajeIA {
  rol: 'usuario' | 'asistente';
  contenido: string;
}

export interface RespuestaAgente {
  respuesta: string;
  herramientas: string[];
  proveedor: string;
  modelo: string;
  modo: 'proveedor' | 'local';
  aviso?: string;
}

export interface ReporteTipo {
  tipo: string;
  nombre: string;
}

export interface ReporteGenerado {
  id: string;
  nombre: string;
  mime: string;
  generado_en: string;
  descarga: string;
  filtros: string;
}

export interface PreferenciasTelegram {
  telegram_activo: boolean;
  categorias: Record<string, boolean>;
  solo_criticas: boolean;
}

export interface EstadoTelegram {
  configurado: boolean;
  vinculado: boolean;
  chat?: string;
  usuario_telegram?: string;
  vinculado_en?: string;
  bot_username?: string;
  preferencias: PreferenciasTelegram;
  categorias_permitidas: string[];
  grupo_activo: boolean;
  grupo_chat?: string;
  grupo_categorias?: string[];
}

export interface EntregaTelegram {
  id: number;
  chat_id: string;
  etiqueta: string;
  estado: 'ENVIADO' | 'ERROR' | 'REINTENTO';
  detalle?: string;
  fecha: string;
}

export interface EventoWs<T = unknown> {
  type: string;
  data: T;
  fecha: string;
}

export interface CodigoTelegram {
  ok: boolean;
  codigo: string;
  expira: string;
  enlace: string;
  instrucciones: string;
}

export interface ReporteRespuesta {
  ok: boolean;
  reporte: ReporteGenerado;
}
