import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { ToastService } from '../../../services/toast.service';

/**
 * ToastContainer — notificaciones en la esquina superior derecha.
 * Región aria-live="polite": los lectores de pantalla anuncian cada toast.
 */
@Component({
  selector: 'app-toast-container',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './toast-container.html',
  styleUrl: './toast-container.scss',
})
export class ToastContainer {
  private readonly toastService = inject(ToastService);
  readonly toasts = this.toastService.toasts.asReadonly();

  cerrar(id: number): void {
    this.toastService.cerrar(id);
  }
}
