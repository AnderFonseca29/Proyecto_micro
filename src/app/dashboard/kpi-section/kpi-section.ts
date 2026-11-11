import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { NumberTicker } from '../../shared/components/number-ticker/number-ticker';
import { StoreService } from '../../services/store.service';
import { tiempoRelativo } from '../../shared/utilidades';

/**
 * KpiSection — tarjetas de indicadores: movimientos hoy, movimientos por
 * minuto, ocupación global y último evento. Entrada escalonada animada.
 */
@Component({
  selector: 'app-kpi-section',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [NumberTicker],
  templateUrl: './kpi-section.html',
  styleUrl: './kpi-section.scss',
})
export class KpiSection {
  readonly store = inject(StoreService);

  readonly totalHoy = this.store.totalHoy.asReadonly();
  readonly porMinuto = this.store.porMinuto;            // computed: ya es de solo lectura
  readonly ocupacionGlobal = this.store.ocupacionGlobal; // computed: ya es de solo lectura
  readonly ultimo = this.store.ultimoMovimiento;         // computed: ya es de solo lectura

  /** "hace X s" reactivo (se recalcula con el reloj del store). */
  readonly ultimoRelativo = computed(() => {
    const m = this.ultimo();
    if (!m) {
      return '—';
    }
    // Toca la señal ahora() para que el computed se reevalúe con el tick.
    this.store.ahora();
    return tiempoRelativo(m.fecha_registro, this.store.ahora());
  });
}
