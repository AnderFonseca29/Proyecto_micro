import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { StoreService } from '../../services/store.service';
import { Deteccion, StandCodigo, Talla } from '../../../models/gestock.models';

/**
 * AuthPanel — cajas detectadas por el ESP32 que esperan autorización.
 * Modo manual: el operador pulsa Autorizar/Rechazar.
 * Modo automático: la página autoriza sola hacia el stand sugerido.
 */
@Component({
  selector: 'app-auth-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './auth-panel.html',
  styleUrl: './auth-panel.scss',
})
export class AuthPanel {
  readonly store = inject(StoreService);
  readonly STANDS: StandCodigo[] = ['A', 'B', 'C'];

  nombreTalla(t: Talla): string {
    return t === 'P' ? 'Pequeña' : t === 'M' ? 'Mediana' : 'Grande';
  }

  autorizar(d: Deteccion, stand?: StandCodigo): void {
    this.store.autorizar(d, stand ?? d.stand);
  }

  rechazar(d: Deteccion): void {
    this.store.rechazar(d);
  }

  alternar(): void {
    this.store.setAuto(!this.store.autoAutorizar());
  }
}
