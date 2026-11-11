import { ChangeDetectionStrategy, Component, computed, effect, input, signal } from '@angular/core';

/**
 * NumberTicker — contador con animación de número (interpolación cúbica
 * vía requestAnimationFrame). Respeta prefers-reduced-motion.
 */
@Component({
  selector: 'app-number-ticker',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './number-ticker.html',
  styleUrl: './number-ticker.scss',
})
export class NumberTicker {
  /** Valor objetivo a mostrar. */
  readonly valor = input.required<number>();
  /** Duración de la animación (ms). */
  readonly duracion = input(700);
  /** Sufijo opcional (%, /min, …). */
  readonly sufijo = input('');

  /** Valor interpolado que se renderiza. */
  readonly mostrado = signal(0);

  private rafId = 0;
  private desde = 0;

  constructor() {
    // Cada cambio de valor dispara una interpolación desde el valor actual.
    effect(() => {
      const objetivo = this.valor();
      const inicio = this.mostrado();
      this.desde = inicio;
      if (objetivo === inicio) {
        return;
      }
      const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      if (reduceMotion) {
        this.mostrado.set(objetivo);
        return;
      }
      cancelAnimationFrame(this.rafId);
      const t0 = performance.now();
      const dur = this.duracion();
      const paso = (t: number) => {
        const p = Math.min(1, (t - t0) / dur);
        const eased = 1 - Math.pow(1 - p, 3); // easeOutCubic
        const actual = this.desde + (objetivo - this.desde) * eased;
        this.mostrado.set(Math.round(actual));
        if (p < 1) {
          this.rafId = requestAnimationFrame(paso);
        }
      };
      this.rafId = requestAnimationFrame(paso);
    });
  }

  ngOnDestroy(): void {
    cancelAnimationFrame(this.rafId);
  }
}
