import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, OnDestroy, computed, inject, signal } from '@angular/core';
import { Subscription, firstValueFrom, interval } from 'rxjs';
import { environment } from '../../environments/environment';
import {
  EstadoClasificacion,
  EstadoIoT,
  EventoClasificacion,
  EventoSensor,
  OrigenRecepcion,
  Producto,
  Recepcion,
  RespuestaRecepcion,
  ResultadoRecepcion,
  SensorEstado,
} from '../../models/gestock.models';
import { RealtimeService } from './realtime.service';

/**
 * RecepcionService — capa de datos del módulo Recepción Inteligente IoT.
 *
 * Estrategia "backend-first":
 *  1. El backend Go (backend/) es la FUENTE DE VERDAD: catálogo, stock,
 *     historial y estado IoT viven en su memoria RAM.
 *  2. Si el backend no responde, se degrada a un catálogo local
 *     (localStorage) y la orden de clasificación se publica DIRECTO por MQTT
 *     hacia el ESP32 (modo sin backend).
 *  3. La confirmación física del ESP32 se refleja en tiempo real por MQTT
 *     (<prefijo>/estado) y, si el backend está en modo simulación, por sondeo.
 *
 * Nunca se marca una clasificación como COMPLETADA sin confirmación real
 * (del ESP32 por MQTT o del backend, que solo la marca al recibirla).
 */

const CLAVE = 'gestock.recepcion.v2';
const MAX_RECEPCIONES = 500;
const INTERVALO_SONDEO_MS = 3000;

/** Catálogo de respaldo: idéntico al del backend, para operar sin él. */
const CATALOGO_DEMO: Producto[] = [
  { codigo: 'PROD-1234', nombre: 'Teclado USB', stock: 10, stock_inicial: 10, activo: true },
  { codigo: 'PROD-1235', nombre: 'Mouse inalámbrico', stock: 15, stock_inicial: 15, activo: true },
  { codigo: 'PROD-1236', nombre: 'Monitor 24 pulgadas', stock: 5, stock_inicial: 5, activo: true },
  { codigo: 'PROD-1237', nombre: 'Cable HDMI', stock: 20, stock_inicial: 20, activo: true },
  { codigo: 'PROD-1238', nombre: 'Memoria USB', stock: 12, stock_inicial: 12, activo: true },
];

/** Forma persistida en localStorage cuando no hay backend. */
interface GuardadoLocal {
  productos: Producto[];
  recepciones: Recepcion[];
  seq: number;
}

@Injectable({ providedIn: 'root' })
export class RecepcionService implements OnDestroy {
  private readonly http = inject(HttpClient);
  private readonly mqtt = inject(RealtimeService);

  private readonly api = environment.apiUrl;
  private suscripcionSondeo: Subscription | null = null;
  private iniciado = false;
  private intentosFallidos = 0;

  // ---------- estado ----------

  /** Catálogo temporal (del backend o el de respaldo). */
  readonly productos = signal<Producto[]>([]);
  /** Historial de recepciones (más reciente primero). */
  readonly recepciones = signal<Recepcion[]>([]);
  /** Estado IoT devuelto por el backend (null si no está disponible). */
  readonly estadoIot = signal<EstadoIoT | null>(null);
  /** true = backend en línea; false = degradado; null = comprobando. */
  readonly backendOk = signal<boolean | null>(null);
  readonly cargando = signal(false);

  /** Última confirmación física recibida del ESP32 (para animar el éxito). */
  readonly ultimaConfirmacion = signal<EventoClasificacion | null>(null);
  /** Marca de tiempo de la última detección del sensor (para el pulso). */
  readonly ultimaDeteccionMs = signal(0);

  /** Respaldo local (solo se usa si el backend está caído). */
  private local: GuardadoLocal = this.cargarGuardado();

  // ---------- estado derivado ----------

  /** Estado IoT consolidado: backend + conexión MQTT + métricas locales. */
  readonly estado = computed<EstadoIoT>(() => {
    const base = this.estadoIot();
    const mqttOk = this.mqtt.estado() === 'en_linea';
    const lista = this.recepciones();
    const hoy = new Date().toISOString().slice(0, 10);

    const registrados = lista.filter((r) => r.estado === 'REGISTRADO').length;
    const desconocidos = lista.filter((r) => r.estado !== 'REGISTRADO').length;
    const unidadesHoy = lista
      .filter((r) => r.estado === 'REGISTRADO' && r.fecha_registro.slice(0, 10) === hoy)
      .reduce((total, r) => total + r.cantidad, 0);

    return {
      sensor: base?.sensor ?? this.sensorVacio(),
      esp32_online: base?.esp32_online ?? false,
      esp32_last_seen: base?.esp32_last_seen,
      mqtt_conectado: mqttOk || (base?.mqtt_conectado ?? false),
      ultimo_mensaje: base?.ultimo_mensaje ?? (mqttOk ? 'MQTT conectado' : 'Sin conexión MQTT'),
      servo: base?.servo ?? 'REPOSO',
      clasificacion: base?.clasificacion ?? '—',
      simulacion: base?.simulacion ?? false,
      total_productos: this.productos().length || (base?.total_productos ?? 0),
      total_recepciones: lista.length || (base?.total_recepciones ?? 0),
      registrados: registrados || (base?.registrados ?? 0),
      desconocidos,
      unidades_hoy: unidadesHoy || (base?.unidades_hoy ?? 0),
    };
  });

  /** Total de recepciones registradas en el historial. */
  readonly totalRecepciones = computed(
    () => this.recepciones().filter((r) => r.estado === 'REGISTRADO').length,
  );

  /** Códigos de producto distintos ya recibidos. */
  readonly productosRecibidos = computed(
    () =>
      new Set(
        this.recepciones()
          .filter((r) => r.estado === 'REGISTRADO')
          .map((r) => r.codigo_producto),
      ).size,
  );

  /** Recepciones que aún esperan confirmación física del ESP32. */
  readonly pendientes = computed(() =>
    this.recepciones().filter((r) => r.clasificacion === 'PENDIENTE'),
  );

  readonly desconocidos = computed(
    () => this.recepciones().filter((r) => r.estado === 'DESCONOCIDO').length,
  );

  constructor() {
    // Confirmaciones y detecciones que llegan por MQTT (en vivo).
    this.mqtt.eventosSensor$.subscribe((e) => this.aplicarEventoSensor(e));
    this.mqtt.eventosClasificacion$.subscribe((e) => this.aplicarEventoClasificacion(e));
  }

  // ---------- ciclo de vida ----------

  /** Carga inicial + sondeo periódico. Idempotente. */
  iniciar(): void {
    if (this.iniciado) {
      return;
    }
    this.iniciado = true;
    void this.recargar();
    this.suscripcionSondeo = interval(INTERVALO_SONDEO_MS).subscribe(
      () => void this.refrescarEstado(),
    );
  }

  ngOnDestroy(): void {
    this.suscripcionSondeo?.unsubscribe();
  }

  // ---------- carga ----------

  /** Recarga catálogo, historial y estado desde el backend. */
  async recargar(): Promise<void> {
    this.cargando.set(true);
    try {
      const [productos, recepciones, estado] = await Promise.all([
        firstValueFrom(this.http.get<{ productos: Producto[] }>(`${this.api}/demo/productos`)),
        firstValueFrom(this.http.get<{ recepciones: Recepcion[] }>(`${this.api}/demo/recepciones`)),
        firstValueFrom(this.http.get<{ estado: EstadoIoT }>(`${this.api}/demo/estado-iot`)),
      ]);
      this.productos.set(productos.productos ?? []);
      this.recepciones.set(recepciones.recepciones ?? []);
      this.estadoIot.set(estado.estado ?? null);
      this.backendOk.set(true);
      this.intentosFallidos = 0;
    } catch {
      this.backendOk.set(false);
      this.usarRespaldoLocal();
    } finally {
      this.cargando.set(false);
    }
  }

  /** Sondeo ligero del estado IoT (y de clasificaciones pendientes). */
  async refrescarEstado(): Promise<void> {
    if (this.backendOk() === false) {
      // Reintenta reconectar con el backend cada ~9 s.
      if (++this.intentosFallidos % 3 === 0) {
        void this.recargar();
      }
      return;
    }
    try {
      const estado = await firstValueFrom(
        this.http.get<{ estado: EstadoIoT }>(`${this.api}/demo/estado-iot`),
      );
      this.estadoIot.set(estado.estado ?? null);
      this.backendOk.set(true);
      this.intentosFallidos = 0;

      // Solo refresca el historial si hay clasificaciones pendientes
      // (en modo simulación el backend las completa solo).
      if (this.pendientes().length > 0) {
        const recepciones = await firstValueFrom(
          this.http.get<{ recepciones: Recepcion[] }>(`${this.api}/demo/recepciones`),
        );
        this.recepciones.set(recepciones.recepciones ?? []);
      }
    } catch {
      this.backendOk.set(false);
      this.usarRespaldoLocal();
    }
  }

  // ---------- flujo principal ----------

  /**
   * Valida un código, suma +1 al stock, registra el ingreso y dispara la
   * orden de clasificación. Backend-first con respaldo local + MQTT.
   */
  async recepcionar(
    codigoEntrada: string,
    origen: OrigenRecepcion,
    cantidad = 1,
  ): Promise<ResultadoRecepcion> {
    const codigo = codigoEntrada.trim();
    const idOperacion = this.nuevoId();
    const fecha = new Date().toISOString();

    if (!codigo) {
      return {
        ok: false,
        estado: 'ERROR',
        codigo: '',
        cantidad,
        mensaje: 'No se leyó ningún código. Acerca la cámara o escríbelo manualmente.',
        mqtt_enviado: false,
        clasificacion: null,
        id_operacion: idOperacion,
        fecha_registro: fecha,
        origen,
      };
    }

    // 1) Backend (fuente de verdad).
    if (this.backendOk() !== false) {
      try {
        const respuesta = await firstValueFrom(
          this.http.post<RespuestaRecepcion>(`${this.api}/demo/recepciones`, {
            codigo_producto: codigo,
            cantidad,
            id_operacion: idOperacion,
            origen,
          }),
        );
        this.backendOk.set(true);
        this.aplicarRespuestaBackend(respuesta);
        return this.normalizarOk(respuesta, codigo, origen, cantidad, idOperacion);
      } catch (err) {
        if (err instanceof HttpErrorResponse && err.status !== 0) {
          // El backend respondió con error (422 desconocido / 400 inválido).
          this.backendOk.set(true);
          return this.normalizarErrorBackend(err, codigo, origen, cantidad, idOperacion);
        }
        // Error de red: se degrada al modo local.
        this.backendOk.set(false);
        this.usarRespaldoLocal();
      }
    }

    // 2) Respaldo local + MQTT directo.
    return this.recepcionarLocal(codigo, origen, cantidad, idOperacion);
  }

  /** Reenvía la orden de clasificación de una operación pendiente. */
  async reintentar(idOperacion: string): Promise<boolean> {
    if (this.backendOk() !== false) {
      try {
        await firstValueFrom(
          this.http.post(`${this.api}/demo/reintentar`, { id_operacion: idOperacion }),
        );
        return true;
      } catch {
        return false;
      }
    }
    const rec = this.local.recepciones.find((r) => r.id_operacion === idOperacion);
    if (!rec) {
      return false;
    }
    return this.mqtt.publicarClasificacion({
      registro: 'exitoso',
      codigo_producto: rec.codigo_producto,
      accion: 'clasificar',
      id_operacion: idOperacion,
    });
  }

  /** Reinicia la demostración (catálogo, stock e historial). */
  async reiniciar(): Promise<void> {
    if (this.backendOk() !== false) {
      try {
        await firstValueFrom(this.http.post(`${this.api}/demo/reiniciar`, {}));
        await this.recargar();
        return;
      } catch {
        this.backendOk.set(false);
      }
    }
    this.local = { productos: structuredClone(CATALOGO_DEMO), recepciones: [], seq: 0 };
    this.guardarGuardado();
    this.usarRespaldoLocal();
    this.ultimaConfirmacion.set(null);
  }

  // ---------- eventos MQTT en vivo ----------

  private aplicarEventoSensor(ev: EventoSensor): void {
    const ahora = new Date().toISOString();
    const previo = this.estadoIot();
    const sensor: SensorEstado = {
      caja_presente: ev.estado,
      ultima_deteccion: ev.estado ? ahora : previo?.sensor.ultima_deteccion,
      dispositivo: ev.dispositivo || previo?.sensor.dispositivo || 'ESP32_GESTOCK',
      detecciones: (previo?.sensor.detecciones ?? 0) + (ev.estado ? 1 : 0),
    };
    this.estadoIot.set({
      ...(previo ?? this.estadoVacio()),
      sensor,
      esp32_online: true,
      esp32_last_seen: ahora,
      mqtt_conectado: true,
      ultimo_mensaje: ev.estado ? 'Caja detectada por el sensor HC-SR04' : 'Banda libre',
    });
    if (ev.estado) {
      this.ultimaDeteccionMs.set(Date.now());
    }
  }

  private aplicarEventoClasificacion(ev: EventoClasificacion): void {
    const ahora = new Date().toISOString();
    const completada = ev.estado === 'clasificacion_completada';
    const fallida = ev.estado === 'error';

    if (!completada && !fallida) {
      // Latido u otro estado: solo confirma que el ESP32 está en línea.
      const previo = this.estadoIot();
      this.estadoIot.set({
        ...(previo ?? this.estadoVacio()),
        esp32_online: true,
        esp32_last_seen: ahora,
        mqtt_conectado: true,
      });
      return;
    }

    const clasificacion: EstadoClasificacion = completada ? 'COMPLETADA' : 'FALLIDA';

    if (ev.id_operacion) {
      this.recepciones.update((lista) =>
        lista.map((r) =>
          r.id_operacion === ev.id_operacion
            ? { ...r, clasificacion, fecha_clasificacion: ahora }
            : r,
        ),
      );
      const local = this.local.recepciones.find((r) => r.id_operacion === ev.id_operacion);
      if (local) {
        local.clasificacion = clasificacion;
        local.fecha_clasificacion = ahora;
        this.guardarGuardado();
      }
    }

    this.ultimaConfirmacion.set({ ...ev, estado: ev.estado });

    const previo = this.estadoIot();
    this.estadoIot.set({
      ...(previo ?? this.estadoVacio()),
      esp32_online: true,
      esp32_last_seen: ahora,
      mqtt_conectado: true,
      servo: 'REPOSO',
      clasificacion,
      ultimo_mensaje: completada
        ? 'Clasificación completada por el ESP32'
        : `Error del ESP32: ${ev.mensaje ?? 'sin detalle'}`,
    });
  }

  // ---------- respaldo local ----------

  private recepcionarLocal(
    codigo: string,
    origen: OrigenRecepcion,
    cantidad: number,
    idOperacion: string,
  ): ResultadoRecepcion {
    const fecha = new Date().toISOString();
    const producto = this.local.productos.find(
      (p) => p.codigo.toLowerCase() === codigo.toLowerCase(),
    );

    if (!producto) {
      this.agregarLocal({
        codigo_producto: codigo,
        nombre_producto: '—',
        cantidad,
        id_operacion: idOperacion,
        estado: 'DESCONOCIDO',
        clasificacion: null,
        origen,
        fecha_registro: fecha,
      });
      return {
        ok: false,
        estado: 'DESCONOCIDO',
        codigo,
        cantidad,
        mensaje: `El producto ${codigo} no existe en el catálogo. No se modificó el stock.`,
        mqtt_enviado: false,
        clasificacion: null,
        id_operacion: idOperacion,
        fecha_registro: fecha,
        origen,
      };
    }

    if (!producto.activo) {
      return {
        ok: false,
        estado: 'ERROR',
        codigo,
        cantidad,
        mensaje: `El producto ${codigo} está inactivo y no puede recibirse.`,
        mqtt_enviado: false,
        clasificacion: null,
        id_operacion: idOperacion,
        fecha_registro: fecha,
        origen,
      };
    }

    // Stock +1 y publicación directa al ESP32 por MQTT.
    producto.stock += cantidad;
    const enviado = this.mqtt.publicarClasificacion({
      registro: 'exitoso',
      codigo_producto: producto.codigo,
      accion: 'clasificar',
      id_operacion: idOperacion,
    });

    this.agregarLocal({
      codigo_producto: producto.codigo,
      nombre_producto: producto.nombre,
      cantidad,
      id_operacion: idOperacion,
      estado: 'REGISTRADO',
      clasificacion: 'PENDIENTE',
      origen,
      fecha_registro: fecha,
    });

    return {
      ok: true,
      estado: 'REGISTRADO',
      codigo: producto.codigo,
      producto: producto.nombre,
      stock: producto.stock,
      stock_inicial: producto.stock_inicial,
      cantidad,
      mensaje: enviado
        ? 'Ingreso registrado (modo local). Orden enviada a la maqueta.'
        : 'Ingreso registrado (modo local). Sin MQTT: clasificación pendiente.',
      mqtt_enviado: enviado,
      clasificacion: 'PENDIENTE',
      id_operacion: idOperacion,
      fecha_registro: fecha,
      origen,
    };
  }

  private agregarLocal(datos: Omit<Recepcion, 'id'>): Recepcion {
    const rec: Recepcion = { id: ++this.local.seq, ...datos };
    this.local.recepciones.unshift(rec);
    if (this.local.recepciones.length > MAX_RECEPCIONES) {
      this.local.recepciones.length = MAX_RECEPCIONES;
    }
    this.guardarGuardado();
    this.recepciones.set([...this.local.recepciones]);
    this.productos.set(structuredClone(this.local.productos));
    return rec;
  }

  private usarRespaldoLocal(): void {
    this.productos.set(structuredClone(this.local.productos));
    this.recepciones.set([...this.local.recepciones]);
  }

  // ---------- normalización de respuestas ----------

  private normalizarOk(
    r: RespuestaRecepcion,
    codigo: string,
    origen: OrigenRecepcion,
    cantidad: number,
    idOperacion: string,
  ): ResultadoRecepcion {
    const rec = r.recepcion;
    const duplicado = !!r.duplicado;
    const enviado = !!r.mqtt_enviado;
    return {
      ok: true,
      estado: 'REGISTRADO',
      codigo: rec?.codigo_producto ?? codigo,
      producto: rec?.nombre_producto ?? r.producto?.nombre,
      stock: r.producto?.stock ?? r.stock,
      stock_inicial: r.producto?.stock_inicial,
      cantidad: rec?.cantidad ?? cantidad,
      mensaje: duplicado
        ? 'Este ingreso ya estaba registrado (no se duplicó el stock).'
        : enviado
          ? 'Ingreso registrado. Orden de clasificación enviada a la maqueta.'
          : 'Ingreso registrado. Clasificación pendiente de la maqueta.',
      mqtt_enviado: enviado,
      clasificacion: rec?.clasificacion ?? 'PENDIENTE',
      id_operacion: rec?.id_operacion ?? idOperacion,
      fecha_registro: rec?.fecha_registro ?? new Date().toISOString(),
      origen: rec?.origen ?? origen,
      duplicado,
    };
  }

  private normalizarErrorBackend(
    err: HttpErrorResponse,
    codigo: string,
    origen: OrigenRecepcion,
    cantidad: number,
    idOperacion: string,
  ): ResultadoRecepcion {
    const cuerpo = (err.error ?? {}) as Partial<RespuestaRecepcion>;
    const estado = cuerpo.estado ?? (err.status === 422 ? 'DESCONOCIDO' : 'ERROR');
    return {
      ok: false,
      estado,
      codigo: cuerpo.codigo ?? codigo,
      cantidad,
      mensaje:
        cuerpo.mensaje ??
        (estado === 'DESCONOCIDO'
          ? `El producto ${codigo} no existe en el catálogo.`
          : 'No se pudo validar el código en el servidor.'),
      mqtt_enviado: false,
      clasificacion: null,
      id_operacion: idOperacion,
      fecha_registro: new Date().toISOString(),
      origen,
    };
  }

  /** Refleja de inmediato en los signals el resultado de un POST válido. */
  private aplicarRespuestaBackend(r: RespuestaRecepcion): void {
    if (r.producto) {
      const actualizado = r.producto;
      this.productos.update((lista) =>
        lista.map((p) => (p.codigo === actualizado.codigo ? actualizado : p)),
      );
    }
    if (r.recepcion && !r.duplicado) {
      const rec = r.recepcion;
      this.recepciones.update((lista) => [
        rec,
        ...lista.filter((x) => x.id_operacion !== rec.id_operacion),
      ]);
    }
  }

  // ---------- persistencia local ----------

  private cargarGuardado(): GuardadoLocal {
    try {
      const crudo = localStorage.getItem(CLAVE);
      if (crudo) {
        const d = JSON.parse(crudo) as GuardadoLocal;
        if (Array.isArray(d.productos) && Array.isArray(d.recepciones)) {
          return d;
        }
      }
    } catch {
      /* datos corruptos: se reinicia el catálogo */
    }
    return { productos: structuredClone(CATALOGO_DEMO), recepciones: [], seq: 0 };
  }

  private guardarGuardado(): void {
    try {
      localStorage.setItem(CLAVE, JSON.stringify(this.local));
    } catch {
      /* almacenamiento lleno o bloqueado */
    }
  }

  // ---------- utilidades ----------

  private nuevoId(): string {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }
    return `op-${Date.now()}-${Math.random().toString(16).slice(2, 10)}`;
  }

  private sensorVacio(): SensorEstado {
    return { caja_presente: false, dispositivo: 'ESP32_GESTOCK', detecciones: 0 };
  }

  private estadoVacio(): EstadoIoT {
    return {
      sensor: this.sensorVacio(),
      esp32_online: false,
      mqtt_conectado: false,
      ultimo_mensaje: 'Sin conexión con el backend ni con el broker',
      servo: 'REPOSO',
      clasificacion: '—',
      simulacion: false,
      total_productos: this.productos().length,
      total_recepciones: this.recepciones().length,
      registrados: 0,
      desconocidos: 0,
      unidades_hoy: 0,
    };
  }
}
