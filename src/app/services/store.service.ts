import { Injectable, computed, inject, signal } from '@angular/core';
import { firstValueFrom, interval } from 'rxjs';
import { ApiService } from './api.service';
import { EventoEsp, RealtimeService } from './realtime.service';
import { ToastService } from './toast.service';
import {
  Deteccion,
  DeviceStatus,
  EstadoConexion,
  Movimiento,
  MovimientoFilters,
  Stand,
  StandCodigo,
} from '../../models/gestock.models';

/**
 * StoreService — única fuente de verdad de la UI (signals).
 * Expone: stands, movimientos (página actual), filtros, estado del
 * dispositivo, conexión en tiempo real y KPIs (computed).
 */
@Injectable({ providedIn: 'root' })
export class StoreService {
  private readonly api = inject(ApiService);
  private readonly realtime = inject(RealtimeService);
  private readonly toasts = inject(ToastService);

  // ---------- estado ----------
  readonly stands = signal<Stand[]>([]);
  readonly movimientos = signal<Movimiento[]>([]);
  readonly totalMovimientos = signal(0);
  readonly deviceStatus = this.realtime.ultimoEstado;
  /** Broker MQTT: null = conectando, true = en línea, false = caído. */
  readonly serverOk = computed<boolean | null>(() => {
    const e = this.realtime.estado();
    return e === 'en_linea' ? true : e === 'conectando' ? null : false;
  });

  /** Cajas detectadas por el ESP32 esperando autorización. */
  readonly pendientes = signal<Deteccion[]>([]);
  /** Si está activo, la página autoriza sola hacia el stand sugerido. */
  readonly autoAutorizar = signal<boolean>(localStorage.getItem('gestock.auto') === '1');

  readonly cargandoStands = signal(false);
  readonly cargandoMovimientos = signal(false);
  readonly cargandoInicial = signal(false);
  readonly errorCarga = signal<string | null>(null);

  readonly filtros = signal<MovimientoFilters>({
    stand: '',
    estado: '',
    origen: '',
    q: '',
    desde: '',
    hasta: '',
    page: 1,
    limit: 25,
  });

  /** Conexión WebSocket en vivo (alias del estado del RealtimeService). */
  readonly conexion: () => EstadoConexion = this.realtime.estado;

  /** Reloj reactivo para tiempos relativos (se actualiza cada 15 s). */
  readonly ahora = signal(Date.now());

  /** Timestamps (ventana de 60 s) de eventos recibidos → KPI por minuto. */
  private readonly feedTs = signal<number[]>([]);

  /** Total de movimientos de hoy (seeds desde /api/kpis, luego en vivo). */
  readonly totalHoy = signal(0);

  // ---------- KPIs (computed) ----------

  /** Ocupación global del almacén en % (0–100). */
  readonly ocupacionGlobal = computed(() => {
    const lista = this.stands();
    if (!lista.length) {
      return 0;
    }
    const cap = lista.reduce((a, s) => a + s.capacidad, 0);
    const ocu = lista.reduce((a, s) => a + s.ocupacion, 0);
    return cap > 0 ? Math.round((ocu / cap) * 100) : 0;
  });

  /** Movimientos por minuto (eventos recibidos en los últimos 60 s). */
  readonly porMinuto = computed(() => {
    const limite = this.ahora() - 60_000;
    return this.feedTs().filter((t) => t >= limite).length;
  });

  /** Último evento recibido (para el KPI "último evento"). */
  readonly ultimoMovimiento = computed<Movimiento | null>(() => {
    const lista = this.movimientos();
    return lista.length ? lista[0] : null;
  });

  /** ¿El ESP32 está online? (false hasta el primer latido). */
  readonly deviceOnline = computed(() => this.deviceStatus()?.online ?? false);

  constructor() {
    // Eventos que publica el ESP32 por MQTT.
    this.realtime.eventos$.subscribe((e) => this.aplicarEvento(e));
    // Reloj reactivo para "hace X s" y para recomputar movimientos/minuto.
    interval(15_000).subscribe(() => this.ahora.set(Date.now()));
  }

  // ---------- carga inicial ----------

  /** Carga todo el estado inicial desde la API REST. */
  async cargarInicial(): Promise<void> {
    this.cargandoInicial.set(true);
    this.errorCarga.set(null);
    try {
      const [stands, kpis] = await Promise.all([
        firstValueFrom(this.api.stands()),
        firstValueFrom(this.api.kpis()),
      ]);
      this.stands.set(stands);
      this.totalHoy.set(kpis.total_hoy);
      await this.cargarMovimientos();
    } catch {
      this.errorCarga.set('No se pudo cargar el estado inicial del sistema.');
      this.toasts.error('Error de carga', 'No se pudieron leer los datos guardados en el navegador.');
    } finally {
      this.cargandoInicial.set(false);
    }
  }

  /** Vuelve a intentar la carga completa (botón de reintento). */
  reintentar(): void {
    this.errorCarga.set(null);
    void this.cargarInicial();
  }

  // ---------- cargas parciales ----------

  async cargarStands(): Promise<void> {
    this.cargandoStands.set(true);
    try {
      this.stands.set(await firstValueFrom(this.api.stands()));
    } catch {
      this.toasts.error('Error', 'No se pudieron obtener los stands.');
    } finally {
      this.cargandoStands.set(false);
    }
  }

  async cargarMovimientos(): Promise<void> {
    this.cargandoMovimientos.set(true);
    try {
      const res = await firstValueFrom(this.api.movimientos(this.filtros()));
      this.movimientos.set(res.data);
      this.totalMovimientos.set(res.total);
    } catch {
      this.toasts.error('Error', 'No se pudo obtener la auditoría de movimientos.');
    } finally {
      this.cargandoMovimientos.set(false);
    }
  }

  // ---------- filtros / paginación ----------

  /** Cambia filtros de la auditoría (vuelve a la página 1 y recarga). */
  async setFiltros(parcial: Partial<MovimientoFilters>): Promise<void> {
    const cambiaPagina = 'page' in parcial;
    this.filtros.update((f) => ({ ...f, ...parcial, ...(cambiaPagina ? {} : { page: 1 }) }));
    await this.cargarMovimientos();
  }

  /** Exporta a CSV con los filtros actuales (abre descarga). */
  exportarCsv(): void {
    this.api.exportarCsv(this.filtros());
  }

  /** Registra un UID en el historial local de filas recién llegadas. */
  private readonly recientes = signal<Set<number>>(new Set());

  /** Ids de filas nuevas (para animación de entrada + resaltado). */
  readonly idsRecientes = this.recientes.asReadonly();

  private marcarReciente(id: number): void {
    this.recientes.update((set) => new Set(set).add(id));
    setTimeout(() => {
      this.recientes.update((set) => {
        const copia = new Set(set);
        copia.delete(id);
        return copia;
      });
    }, 4000);
  }

  // ---------- eventos del ESP32 + autorización ----------

  private contadorRecepcion = 0;

  /** Reacciona a lo que publica el ESP32. */
  private aplicarEvento(e: EventoEsp): void {
    switch (e.evt) {
      case 'DETECTADO': {
        if (!e.talla || !e.stand) {
          return;
        }
        const det: Deteccion = {
          id: e.id ?? `CAJA-${++this.contadorRecepcion}`,
          talla: e.talla,
          stand: e.stand,
          recibida: Date.now(),
        };
        this.pendientes.update((l) => [det, ...l.filter((x) => x.id !== det.id)]);
        if (this.autoAutorizar()) {
          this.autorizar(det);
        } else {
          this.toasts.info('Caja detectada', `Tamaño ${det.talla} → stand ${det.stand}. Esperando tu autorización.`);
        }
        break;
      }
      case 'CLASIFICADO': {
        if (!e.stand) {
          return;
        }
        this.quitarPendiente(e.id);
        this.registrarYMostrar(e.id ?? 'SIN-ID', e.stand, 'CLASIFICADO', e.talla, e.origen ?? 'hardware');
        break;
      }
      case 'TIMEOUT':
        this.quitarPendiente(e.id);
        this.toasts.aviso('Caja sin autorizar', 'El ESP32 dejó de esperar y liberó la caja.');
        break;
      default:
        break;
    }
  }

  /** Autoriza una caja detectada: manda SORT al ESP32 (si el stand tiene espacio). */
  autorizar(det: Deteccion, stand: StandCodigo = det.stand): void {
    const s = this.stands().find((x) => x.codigo === stand);
    if (s && s.ocupacion >= s.capacidad) {
      this.api.comando('REJECT', { id: det.id }).subscribe();
      this.quitarPendiente(det.id);
      this.registrarYMostrar(det.id, stand, 'STAND_LLENO', det.talla, 'hardware');
      return;
    }
    this.api.clasificar(stand, det.id).subscribe();
    // El movimiento se guarda cuando el ESP32 confirma CLASIFICADO.
    this.quitarPendiente(det.id);
  }

  /** Rechaza una caja detectada: el ESP32 la libera sin mover el servo. */
  rechazar(det: Deteccion): void {
    this.api.comando('REJECT', { id: det.id }).subscribe();
    this.quitarPendiente(det.id);
    this.registrarYMostrar(det.id, det.stand, 'RECHAZADO', det.talla, 'web');
  }

  setAuto(valor: boolean): void {
    this.autoAutorizar.set(valor);
    localStorage.setItem('gestock.auto', valor ? '1' : '0');
    if (valor) {
      [...this.pendientes()].forEach((d) => this.autorizar(d));
    }
  }

  private quitarPendiente(id?: string): void {
    if (id) {
      this.pendientes.update((l) => l.filter((x) => x.id !== id));
    }
  }

  private registrarYMostrar(
    id: string,
    stand: StandCodigo,
    estado: string,
    talla: Movimiento['talla'],
    origen: 'hardware' | 'web',
  ): void {
    const m = this.api.registrar({ id, stand, estado, talla, origen });
    this.api.stands().subscribe((v) => this.stands.set(v));
    this.registrarMovimiento(m);
  }

  /** Procesa un movimiento nuevo: KPIs, fila viva (si aplica) y avisos. */
  private registrarMovimiento(m: Movimiento): void {
    this.feedTs.update((ts) => [...ts, Date.now()]);
    if (this.esDeHoy(m.fecha_registro)) {
      this.totalHoy.update((n) => n + 1);
    }
    this.marcarReciente(m.id);

    const f = this.filtros();
    if (f.page === 1 && this.coincideFiltros(m, f)) {
      // Fila nueva al inicio de la tabla (recortada al límite de página).
      this.movimientos.update((lista) => [m, ...lista].slice(0, f.limit));
      this.totalMovimientos.update((n) => Math.max(n, 0) + (this.sinFiltros(f) ? 1 : 0));
    } else if (this.sinFiltros(f)) {
      this.totalMovimientos.update((n) => n + 1);
    }

    if (m.estado === 'STAND_LLENO') {
      this.toasts.aviso('Stand lleno', `Se rechazó ${m.rfid} → stand ${m.stand}.`);
    }
  }

  /** ¿El movimiento coincide con los filtros activos de la tabla? */
  private coincideFiltros(m: Movimiento, f: MovimientoFilters): boolean {
    if (f.stand && m.stand !== f.stand) {
      return false;
    }
    if (f.estado && m.estado !== f.estado) {
      return false;
    }
    if (f.origen && m.origen !== f.origen) {
      return false;
    }
    if (f.q && !m.rfid.toLowerCase().includes(f.q.toLowerCase())) {
      return false;
    }
    if (f.desde || f.hasta) {
      const dia = m.fecha_registro.slice(0, 10);
      if (f.desde && dia < f.desde) {
        return false;
      }
      if (f.hasta && dia > f.hasta) {
        return false;
      }
    }
    return true;
  }

  private sinFiltros(f: MovimientoFilters): boolean {
    return !f.stand && !f.estado && !f.origen && !f.q && !f.desde && !f.hasta;
  }

  private esDeHoy(iso: string): boolean {
    const dia = iso.slice(0, 10);
    const hoy = new Date().toISOString().slice(0, 10);
    return dia === hoy;
  }
}
