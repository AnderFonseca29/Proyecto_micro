import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { Header } from './layout/header/header';
import { ToastContainer } from './shared/components/toast-container/toast-container';
import { RealtimeService } from './services/realtime.service';
import { StoreService } from './services/store.service';

/**
 * App — shell de GESTOCK: cabecera con indicadores en vivo, banner de
 * reconexión WebSocket, vista del router y contenedor de toasts.
 */
@Component({
  selector: 'app-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterOutlet, Header, ToastContainer],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App {
  readonly store = inject(StoreService);
  private readonly realtime = inject(RealtimeService);

  /** ¿Se muestra el banner de conexión en tiempo real? */
  readonly mostrarBanner = computed(() => this.store.conexion() !== 'en_linea');

  /** Texto del banner según el estado de la conexión WebSocket. */
  readonly mensajeBanner = computed(() => {
    switch (this.store.conexion()) {
      case 'conectando':
        return 'Conectando con el broker MQTT…';
      case 'reconectando':
        return 'Conexión con el broker perdida. Reintentando automáticamente…';
      default:
        return 'Sin conexión con el broker MQTT. No llegarán eventos del ESP32 hasta reconectar.';
    }
  });

  /** Reconexión inmediata del WebSocket (botón del banner). */
  reintentarConexion(): void {
    this.realtime.reintentarAhora();
  }
}
