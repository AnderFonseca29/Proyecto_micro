import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { StatusDot } from '../../shared/components/status-dot/status-dot';
import { StoreService } from '../../services/store.service';

/**
 * Header — logo, nombre del sistema e indicadores en vivo:
 * Servidor (REST) y MQTT/Dispositivo (ESP32). Verde/ámbar/rojo con pulso.
 */
@Component({
  selector: 'app-header',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [StatusDot],
  templateUrl: './header.html',
  styleUrl: './header.scss',
})
export class Header {
  private readonly store = inject(StoreService);

  /** null = aún sin sondeo; true = saludable; false = caído. */
  readonly estadoServidor = computed<'ok' | 'warn' | 'error'>(() => {
    const ok = this.store.serverOk();
    if (ok === null) {
      return 'warn';
    }
    return ok ? 'ok' : 'error';
  });

  /** Dispositivo: verde online, ámbar sin datos, rojo offline (LWT). */
  readonly estadoDispositivo = computed<'ok' | 'warn' | 'error'>(() => {
    const ds = this.store.deviceStatus();
    if (!ds) {
      return 'warn';
    }
    return ds.online ? 'ok' : 'error';
  });

  readonly etiquetaServidor = computed(() => {
    const ok = this.store.serverOk();
    if (ok === null) {
      return 'Broker: conectando…';
    }
    return ok ? 'Broker MQTT en línea' : 'Broker MQTT caído';
  });

  readonly etiquetaDispositivo = computed(() => {
    const ds = this.store.deviceStatus();
    if (!ds) {
      return 'ESP32: sin datos';
    }
    return ds.online ? 'ESP32 en línea' : 'ESP32 offline';
  });
}
