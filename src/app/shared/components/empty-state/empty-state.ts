import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * EmptyState — estado vacío con ícono, título y mensaje opcional.
 */
@Component({
  selector: 'app-empty-state',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './empty-state.html',
  styleUrl: './empty-state.scss',
})
export class EmptyState {
  readonly titulo = input.required<string>();
  readonly mensaje = input('');
}
