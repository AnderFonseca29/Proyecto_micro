import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Subject, debounceTime, distinctUntilChanged } from 'rxjs';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { EmptyState } from '../../shared/components/empty-state/empty-state';
import { StoreService } from '../../services/store.service';
import { ToastService } from '../../services/toast.service';
import { tiempoExacto, tiempoRelativo } from '../../shared/utilidades';
import { Movimiento } from '../../../models/gestock.models';

/** Paginación derivada del total y de la página actual. */
interface Paginacion {
  pagina: number;
  paginas: number;
  total: number;
  desde: number;
  hasta: number;
  hayAnterior: boolean;
  haySiguiente: boolean;
}

/**
 * AuditTable — tabla de auditoría en tiempo real:
 * filtros (stand/estado/origen/UID con debounce/fechas), filas con chips de
 * UID copiables, badges por stand y estado, hora exacta + relativa,
 * paginación, exportación CSV y animación de filas nuevas (vía WebSocket).
 */
@Component({
  selector: 'app-audit-table',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [EmptyState],
  templateUrl: './audit-table.html',
  styleUrl: './audit-table.scss',
})
export class AuditTable {
  private readonly toast = inject(ToastService);
  readonly store = inject(StoreService);

  // Estado del store expuesto a la plantilla.
  readonly filas = this.store.movimientos.asReadonly();
  readonly cargando = this.store.cargandoMovimientos.asReadonly();
  readonly filtros = this.store.filtros.asReadonly();
  readonly idsRecientes = this.store.idsRecientes;

  /** Texto del buscador (local; se propaga al store con debounce). */
  readonly busqueda = signal('');

  /** Códigos de stand para el filtro (o A/B/C si aún no cargan). */
  readonly codigos = computed(() => {
    const stands = this.store.stands();
    return stands.length ? stands.map((s) => s.codigo) : (['A', 'B', 'C'] as const);
  });

  readonly ESTADOS = ['CLASIFICADO', 'STAND_LLENO', 'RECHAZADO'] as const;

  /** ¿Hay conexión WebSocket viva (chip "EN VIVO")? */
  readonly enVivo = computed(() => this.store.conexion() === 'en_linea');

  /** ¿Algún filtro aplicado (para "limpiar" y mensajes de vacío)? */
  readonly tieneFiltros = computed(() => {
    const f = this.filtros();
    return !!(f.stand || f.estado || f.origen || f.q || f.desde || f.hasta);
  });

  /** Paginación calculada a partir del total y el límite de página. */
  readonly paginacion = computed<Paginacion>(() => {
    const f = this.filtros();
    const total = this.store.totalMovimientos();
    const paginas = Math.max(1, Math.ceil(total / f.limit));
    const pagina = Math.min(Math.max(1, f.page), paginas);
    return {
      pagina,
      paginas,
      total,
      desde: total === 0 ? 0 : (pagina - 1) * f.limit + 1,
      hasta: Math.min(total, pagina * f.limit),
      hayAnterior: pagina > 1,
      haySiguiente: pagina < paginas,
    };
  });

  readonly vacioTitulo = computed(() =>
    this.tieneFiltros() ? 'Sin resultados' : 'Sin movimientos todavía',
  );

  readonly vacioMensaje = computed(() =>
    this.tieneFiltros()
      ? 'Ningún movimiento coincide con los filtros aplicados. Prueba ampliar el rango de fechas o límpialos.'
      : 'Las cajas detectadas y clasificadas aparecerán aquí. Los datos se guardan en este navegador.',
  );

  /** Búsqueda por UID con debounce de 350 ms (flujo privado). */
  private readonly entradaBusqueda$ = new Subject<string>();

  constructor() {
    this.entradaBusqueda$
      .pipe(debounceTime(350), distinctUntilChanged(), takeUntilDestroyed())
      .subscribe((q) => void this.store.setFiltros({ q }));
  }

  // ---------- filtros ----------

  escribir(valor: string): void {
    this.busqueda.set(valor);
    this.entradaBusqueda$.next(valor);
  }

  filtrarStand(v: string): void { void this.store.setFiltros({ stand: v }); }
  filtrarEstado(v: string): void { void this.store.setFiltros({ estado: v }); }
  filtrarOrigen(v: string): void { void this.store.setFiltros({ origen: v }); }
  filtrarDesde(v: string): void { void this.store.setFiltros({ desde: v }); }
  filtrarHasta(v: string): void { void this.store.setFiltros({ hasta: v }); }

  /** Restablece todos los filtros y el buscador. */
  limpiar(): void {
    this.busqueda.set('');
    this.entradaBusqueda$.next('');
    void this.store.setFiltros({
      stand: '',
      estado: '',
      origen: '',
      q: '',
      desde: '',
      hasta: '',
    });
  }

  // ---------- paginación ----------

  irPagina(p: number): void {
    void this.store.setFiltros({ page: Math.min(Math.max(1, p), this.paginacion().paginas) });
  }

  // ---------- presentación ----------

  /** Clase de badge según el estado del movimiento. */
  claseEstado(estado: string): 'ok' | 'danger' | 'warn' {
    if (estado === 'CLASIFICADO') {
      return 'ok';
    }
    if (estado === 'STAND_LLENO') {
      return 'danger';
    }
    return 'warn';
  }

  /** "hace X s" reactivo (se recalcula con el reloj del store cada 15 s). */
  relativo(iso: string): string {
    this.store.ahora();
    return tiempoRelativo(iso, this.store.ahora());
  }

  /** Marca de tiempo exacta para el title del tooltip. */
  exacto(iso: string): string {
    return tiempoExacto(iso);
  }

  /** Copia un UID al portapapeles (con fallback para HTTP sin contexto seguro). */
  async copiarUid(m: Movimiento): Promise<void> {
    const ok = await this.copiarTexto(m.rfid);
    if (ok) {
      this.toast.info('UID copiado al portapapeles', m.rfid);
    } else {
      this.toast.error('No se pudo copiar', 'Selecciona el UID manualmente.');
    }
  }

  private async copiarTexto(texto: string): Promise<boolean> {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(texto);
        return true;
      }
      return this.copiarLegado(texto);
    } catch {
      return this.copiarLegado(texto);
    }
  }

  /** Fallback para contextos no seguros (HTTP en LAN). */
  private copiarLegado(texto: string): boolean {
    const area = document.createElement('textarea');
    area.value = texto;
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    area.remove();
    return ok;
  }
}
