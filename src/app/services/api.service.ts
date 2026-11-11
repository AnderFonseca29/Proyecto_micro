import { Injectable, inject } from '@angular/core';
import { Observable, of } from 'rxjs';
import {
  ComandoResponse,
  DeviceStatus,
  HealthStatus,
  Kpis,
  Movimiento,
  MovimientoFilters,
  PageResult,
  Stand,
  StandCodigo,
  Talla,
} from '../../models/gestock.models';
import { RealtimeService } from './realtime.service';

/** Forma guardada en el navegador (localStorage). */
interface Guardado {
  stands: Stand[];
  movimientos: Movimiento[];
  seq: number;
}

const CLAVE = 'gestock.datos.v1';
const MAX_MOVIMIENTOS = 2000;

/**
 * ApiService (modo SIN backend).
 * Misma interfaz que antes, pero los datos viven en localStorage del
 * navegador y los comandos salen por MQTT hacia el ESP32.
 */
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly mqtt = inject(RealtimeService);
  private datos: Guardado = this.cargar();

  // ---------- persistencia ----------

  private cargar(): Guardado {
    try {
      const crudo = localStorage.getItem(CLAVE);
      if (crudo) {
        const d = JSON.parse(crudo) as Guardado;
        if (Array.isArray(d.stands) && Array.isArray(d.movimientos)) {
          return d;
        }
      }
    } catch {
      /* datos corruptos: se reinician */
    }
    const ahora = new Date().toISOString();
    return {
      seq: 0,
      movimientos: [],
      stands: (['A', 'B', 'C'] as StandCodigo[]).map((codigo) => ({
        codigo,
        nombre: `Stand ${codigo}`,
        capacidad: 50,
        ocupacion: 0,
        actualizado_en: ahora,
      })),
    };
  }

  private guardar(): void {
    try {
      localStorage.setItem(CLAVE, JSON.stringify(this.datos));
    } catch {
      /* almacenamiento lleno o bloqueado */
    }
  }

  // ---------- monitoreo ----------

  health(): Observable<HealthStatus> {
    const ok = this.mqtt.estado() === 'en_linea';
    return of({ status: ok ? 'ok' : 'down', db: true, mqtt: ok, ts: Date.now() });
  }

  dispositivo(): Observable<DeviceStatus> {
    return of(
      this.mqtt.ultimoEstado() ?? { device_id: 'esp32', online: false, last_seen: '' },
    );
  }

  kpis(): Observable<Kpis> {
    const hoy = new Date().toISOString().slice(0, 10);
    const lista = this.datos.movimientos;
    return of({
      total_hoy: lista.filter((m) => m.fecha_registro.slice(0, 10) === hoy).length,
      ultimo: lista[0] ?? null,
    });
  }

  // ---------- stands ----------

  stands(): Observable<Stand[]> {
    return of(structuredClone(this.datos.stands));
  }

  patchStand(codigo: string, capacidad: number): Observable<Stand> {
    const s = this.buscar(codigo);
    s.capacidad = capacidad;
    s.actualizado_en = new Date().toISOString();
    this.guardar();
    return of({ ...s });
  }

  resetStand(codigo: string): Observable<Stand> {
    const s = this.buscar(codigo);
    s.ocupacion = 0;
    s.actualizado_en = new Date().toISOString();
    this.guardar();
    return of({ ...s });
  }

  private buscar(codigo: string): Stand {
    const s = this.datos.stands.find((x) => x.codigo === codigo);
    if (!s) {
      throw new Error(`Stand ${codigo} no existe`);
    }
    return s;
  }

  // ---------- auditoría ----------

  movimientos(f: MovimientoFilters): Observable<PageResult<Movimiento>> {
    const q = f.q.trim().toLowerCase();
    const filtrados = this.datos.movimientos.filter((m) => {
      const dia = m.fecha_registro.slice(0, 10);
      return (
        (!f.stand || m.stand === f.stand) &&
        (!f.estado || m.estado === f.estado) &&
        (!f.origen || m.origen === f.origen) &&
        (!q || m.rfid.toLowerCase().includes(q)) &&
        (!f.desde || dia >= f.desde) &&
        (!f.hasta || dia <= f.hasta)
      );
    });
    const inicio = (f.page - 1) * f.limit;
    return of({
      data: filtrados.slice(inicio, inicio + f.limit),
      total: filtrados.length,
      page: f.page,
      limit: f.limit,
    });
  }

  /** Genera y descarga un CSV con los movimientos que cumplen los filtros. */
  exportarCsv(f: MovimientoFilters): void {
    let todos: Movimiento[] = [];
    this.movimientos({ ...f, page: 1, limit: MAX_MOVIMIENTOS }).subscribe((r) => (todos = r.data));
    const filas = [
      'id,caja,talla,stand,estado,origen,fecha_registro',
      ...todos.map((m) =>
        [m.id, m.rfid, m.talla ?? '', m.stand, m.estado, m.origen, m.fecha_registro].join(','),
      ),
    ];
    const blob = new Blob([filas.join('\n')], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `gestock-movimientos-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  /**
   * Registra un evento: suma ocupación si fue CLASIFICADO y lo guarda.
   * Devuelve el movimiento creado.
   */
  registrar(datos: {
    id: string;
    stand: StandCodigo;
    estado: string;
    talla?: Talla;
    origen: 'hardware' | 'web';
  }): Movimiento {
    const m: Movimiento = {
      id: ++this.datos.seq,
      rfid: datos.id,
      stand: datos.stand,
      estado: datos.estado,
      talla: datos.talla,
      origen: datos.origen,
      device_id: 'esp32-01',
      fecha_registro: new Date().toISOString(),
    };
    if (datos.estado === 'CLASIFICADO') {
      const s = this.datos.stands.find((x) => x.codigo === datos.stand);
      if (s) {
        s.ocupacion = Math.min(s.ocupacion + 1, s.capacidad);
        s.actualizado_en = m.fecha_registro;
      }
    }
    this.datos.movimientos.unshift(m);
    this.datos.movimientos.length = Math.min(this.datos.movimientos.length, MAX_MOVIMIENTOS);
    this.guardar();
    return m;
  }

  // ---------- comandos hacia el ESP32 (MQTT) ----------

  /** Autoriza una caja (o clasifica manualmente) hacia un stand. */
  clasificar(stand: string, rfid?: string): Observable<ComandoResponse> {
    const id = rfid || `WEB-${Date.now().toString(36).toUpperCase()}`;
    const cmd_id = this.mqtt.enviar({ cmd: 'SORT', stand, id });
    return of({ cmd_id, stand, rfid: id, cmd: 'SORT' });
  }

  /** SERVO_TEST / REJECT. */
  comando(cmd: string, extra: Record<string, string> = {}): Observable<ComandoResponse> {
    const cmd_id = this.mqtt.enviar({ cmd, ...extra });
    return of({ cmd_id, cmd });
  }
}
