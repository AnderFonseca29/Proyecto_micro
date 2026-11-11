import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ProgressRing } from '../../shared/components/progress-ring/progress-ring';
import { ConfirmModal } from '../../shared/components/confirm-modal/confirm-modal';
import { EmptyState } from '../../shared/components/empty-state/empty-state';
import { ApiService } from '../../services/api.service';
import { StoreService } from '../../services/store.service';
import { ToastService } from '../../services/toast.service';
import { Stand } from '../../../models/gestock.models';

/** Estado visual de un stand derivado de su ocupación. */
interface StandVista {
  codigo: string;
  stand: Stand;
  porcentaje: number;
  etiqueta: 'Disponible' | 'Casi lleno' | 'Lleno';
  clase: 'ok' | 'warn' | 'danger';
  color: string;
}

/**
 * StandsSection — tarjetas A/B/C con anillo de progreso SVG animado,
 * ocupación/capacidad, estado, botón "Clasificar aquí", edición de capacidad
 * y reinicio de ocupación (con modales de confirmación).
 */
@Component({
  selector: 'app-stands-section',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ProgressRing, ConfirmModal, EmptyState],
  templateUrl: './stands-section.html',
  styleUrl: './stands-section.scss',
})
export class StandsSection {
  private readonly api = inject(ApiService);
  private readonly toast = inject(ToastService);
  readonly store = inject(StoreService);

  readonly stands = this.store.stands.asReadonly();
  readonly cargando = this.store.cargandoStands.asReadonly();

  /** Stand que se está clasificando desde la tarjeta (spinner del botón). */
  readonly clasificando = signal<string | null>(null);
  /** Menú desplegado por stand (código o null). */
  readonly menuAbierto = signal<string | null>(null);

  // Modales
  readonly modalEditar = signal<Stand | null>(null);
  readonly modalReset = signal<Stand | null>(null);
  readonly capacidadEditada = signal<number>(50);
  readonly guardando = signal(false);

  /** Vista enriquecida por stand (color, estado y umbral ≥ 80 %). */
  readonly vistas = computed<StandVista[]>(() =>
    this.stands().map((stand) => {
      const porcentaje = stand.capacidad > 0
        ? Math.round((stand.ocupacion / stand.capacidad) * 100)
        : 0;
      const etiqueta: StandVista['etiqueta'] =
        porcentaje >= 100 ? 'Lleno' : porcentaje >= 80 ? 'Casi lleno' : 'Disponible';
      const clase: StandVista['clase'] =
        porcentaje >= 100 ? 'danger' : porcentaje >= 80 ? 'warn' : 'ok';
      const color = clase === 'danger' ? 'var(--danger)'
        : clase === 'warn' ? 'var(--warn)'
        : `var(--stand-${stand.codigo.toLowerCase()})`;
      return { codigo: stand.codigo, stand, porcentaje, etiqueta, clase, color };
    }),
  );

  /** Clasifica hacia el stand indicado (comando SORT vía backend). */
  async clasificar(stand: Stand): Promise<void> {
    if (this.clasificando() !== null || stand.ocupacion >= stand.capacidad) {
      return;
    }
    this.clasificando.set(stand.codigo);
    try {
      const res = await firstValueFrom(this.api.clasificar(stand.codigo));
      this.toast.exito(`Comando enviado al stand ${stand.codigo}`, `cmd_id ${res.cmd_id} · ${res.rfid}`);
    } catch (err) {
      this.toast.error('No se pudo clasificar', this.mensajeError(err, stand.codigo));
    } finally {
      this.clasificando.set(null);
    }
  }

  abrirEditar(stand: Stand): void {
    this.menuAbierto.set(null);
    this.capacidadEditada.set(stand.capacidad);
    this.modalEditar.set(stand);
  }

  abrirReset(stand: Stand): void {
    this.menuAbierto.set(null);
    this.modalReset.set(stand);
  }

  async guardarCapacidad(): Promise<void> {
    const stand = this.modalEditar();
    if (!stand) {
      return;
    }
    const capacidad = this.capacidadEditada();
    if (!Number.isFinite(capacidad) || capacidad < 1 || capacidad > 100000) {
      this.toast.error('Capacidad inválida', 'Debe ser un número entre 1 y 100000.');
      return;
    }
    this.guardando.set(true);
    try {
      await firstValueFrom(this.api.patchStand(stand.codigo, capacidad));
      this.toast.exito(`Stand ${stand.codigo} actualizado`, `Nueva capacidad: ${capacidad}.`);
      this.modalEditar.set(null);
      await this.store.cargarStands();
    } catch (err) {
      this.toast.error('No se pudo actualizar', this.mensajeError(err, stand.codigo));
    } finally {
      this.guardando.set(false);
    }
  }

  async confirmarReset(): Promise<void> {
    const stand = this.modalReset();
    if (!stand) {
      return;
    }
    this.guardando.set(true);
    try {
      await firstValueFrom(this.api.resetStand(stand.codigo));
      this.toast.exito(`Stand ${stand.codigo} reiniciado`, 'Ocupación en 0.');
      this.modalReset.set(null);
      await this.store.cargarStands();
    } catch (err) {
      this.toast.error('No se pudo reiniciar', this.mensajeError(err, stand.codigo));
    } finally {
      this.guardando.set(false);
    }
  }

  private mensajeError(err: unknown, stand: string): string {
    const e = err as { error?: { error?: { code?: string; message?: string } } };
    const code = e?.error?.error?.code;
    if (code === 'STAND_LLENO') {
      return `El stand ${stand} está lleno.`;
    }
    return e?.error?.error?.message ?? 'Error de comunicación con el servidor.';
  }
}
