import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/**
 * ProgressRing — anillo de progreso SVG animado por stroke-dashoffset.
 * El color cambia suavemente al cruzar umbrales (ok → warn → danger),
 * definido por el componente que lo usa vía la señal `color`.
 */
@Component({
  selector: 'app-progress-ring',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './progress-ring.html',
  styleUrl: './progress-ring.scss',
})
export class ProgressRing {
  /** Porcentaje 0–100. */
  readonly porcentaje = input.required<number>();
  /** Color del aro (CSS color o variable). */
  readonly color = input<string>('var(--primary)');
  /** Radio del anillo (px del viewBox). */
  readonly radio = input(44);
  /** Grosor del trazo. */
  readonly grosor = input(9);

  /** Lado del viewBox = diámetro + grosor. */
  readonly tamano = computed(() => (this.radio() + this.grosor()) * 2);
  /** Longitud de la circunferencia. */
  readonly circunferencia = computed(() => 2 * Math.PI * this.radio());
  /** Desplazamiento del trazo (animado por CSS transition). */
  readonly offset = computed(() => {
    const pct = Math.min(100, Math.max(0, this.porcentaje()));
    return this.circunferencia() * (1 - pct / 100);
  });
}
