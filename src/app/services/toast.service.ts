import { Injectable, signal } from '@angular/core';

/** Tipos de toast soportados por el contenedor global. */
export type TipoToast = 'success' | 'error' | 'info' | 'warn';

/** Un toast activo en pantalla. */
export interface Toast {
  id: number;
  tipo: TipoToast;
  titulo: string;
  mensaje?: string;
  /** Duración de la barra de progreso (ms) — coincide con el autocierre. */
  duracion: number;
  /** Marca la animación de salida (reservado para el cierre manual). */
  cerrando?: boolean;
}

/**
 * ToastService — notificaciones efímeras (esquina superior derecha).
 * El contenedor las renderiza con aria-live="polite" y las descarta solas.
 */
@Injectable({ providedIn: 'root' })
export class ToastService {
  /** Toasts visibles (máx. 5 simultáneos). */
  readonly toasts = signal<Toast[]>([]);

  private secuencia = 0;

  /** Muestra un toast con autocierre. */
  mostrar(tipo: TipoToast, titulo: string, mensaje?: string, duracionMs = 4500): void {
    const toast: Toast = {
      id: ++this.secuencia,
      tipo,
      titulo,
      mensaje,
      duracion: duracionMs,
    };
    this.toasts.update((lista) => [...lista, toast].slice(-5));
    setTimeout(() => this.cerrar(toast.id), duracionMs);
  }

  exito(titulo: string, mensaje?: string): void { this.mostrar('success', titulo, mensaje); }
  error(titulo: string, mensaje?: string, duracionMs = 6000): void {
    this.mostrar('error', titulo, mensaje, duracionMs);
  }
  aviso(titulo: string, mensaje?: string): void { this.mostrar('warn', titulo, mensaje); }
  info(titulo: string, mensaje?: string): void { this.mostrar('info', titulo, mensaje); }

  /** Cierra un toast manualmente (clic en la X). */
  cerrar(id: number): void {
    this.toasts.update((lista) => lista.filter((t) => t.id !== id));
  }
}
