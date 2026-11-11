import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  HostListener,
  input,
  output,
  ViewChild,
} from '@angular/core';

/**
 * ConfirmModal — modal de confirmación reutilizable con backdrop-blur
 * animado, cierre por Escape/backdrop y foco atrapado en el botón principal.
 */
@Component({
  selector: 'app-confirm-modal',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './confirm-modal.html',
  styleUrl: './confirm-modal.scss',
})
export class ConfirmModal {
  /** Visibilidad del modal. */
  readonly abierto = input.required<boolean>();
  /** Título del modal. */
  readonly titulo = input.required<string>();
  /** Mensaje descriptivo. */
  readonly mensaje = input('');
  /** Texto del botón de confirmación. */
  readonly textoConfirmar = input('Confirmar');
  /** Estilo peligroso (rojo) para acciones destructivas. */
  readonly peligro = input(false);
  /** Deshabilita el botón de confirmación durante una operación en curso. */
  readonly guardando = input(false);

  /** Se emite al confirmar. */
  readonly confirmar = output<void>();
  /** Se emite al cerrar sin confirmar. */
  readonly cerrar = output<void>();

  @ViewChild('btnConfirmar', { static: false })
  private readonly btnConfirmar!: ElementRef<HTMLButtonElement>;

  /** Escape cierra el modal (accesibilidad por teclado). */
  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.abierto()) {
      this.cerrar.emit();
    }
  }

  /** Al abrirse, mueve el foco al botón principal. */
  enfocar(): void {
    this.btnConfirmar?.nativeElement.focus();
  }
}
