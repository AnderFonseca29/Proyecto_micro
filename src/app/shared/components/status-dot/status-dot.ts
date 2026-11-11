import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * StatusDot — indicador en vivo con punto pulsante.
 * verde (ok) / ámbar (warn) / rojo (error) + etiqueta descriptiva.
 */
@Component({
  selector: 'app-status-dot',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './status-dot.html',
  styleUrl: './status-dot.scss',
})
export class StatusDot {
  /** Estado del indicador. */
  readonly estado = input.required<'ok' | 'warn' | 'error' | 'apagado'>();
  /** Etiqueta junto al punto. */
  readonly etiqueta = input.required<string>();
  /** Habilita el pulso animado (indicador "en vivo"). */
  readonly pulsante = input(true);
}
