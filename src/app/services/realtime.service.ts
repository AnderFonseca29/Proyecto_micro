import { Injectable, OnDestroy, signal } from '@angular/core';
import { Subject } from 'rxjs';
import mqtt, { MqttClient } from 'mqtt';
import { environment } from '../../environments/environment';
import { DeviceStatus, EstadoConexion } from '../../models/gestock.models';

/** Evento publicado por el ESP32 en <prefijo>/evento. */
export interface EventoEsp {
  evt: 'DETECTADO' | 'CLASIFICADO' | 'TIMEOUT' | 'RECHAZADO' | 'SERVO_TEST';
  id?: string;
  talla?: 'P' | 'M' | 'G';
  stand?: 'A' | 'B' | 'C';
  origen?: 'hardware' | 'web';
}

/**
 * RealtimeService — cliente MQTT (WebSocket seguro) hacia el broker.
 * Escucha <prefijo>/evento y <prefijo>/status; envía a <prefijo>/cmd.
 * mqtt.js reconecta solo; aquí solo reflejamos el estado en un Signal.
 */
@Injectable({ providedIn: 'root' })
export class RealtimeService implements OnDestroy {
  readonly estado = signal<EstadoConexion>('conectando');
  /** Último estado publicado por el ESP32 (retenido en el broker). */
  readonly ultimoEstado = signal<DeviceStatus | null>(null);

  private readonly eventos = new Subject<EventoEsp>();
  readonly eventos$ = this.eventos.asObservable();

  private readonly tEvento = `${environment.prefijo}/evento`;
  private readonly tStatus = `${environment.prefijo}/status`;
  private readonly tCmd = `${environment.prefijo}/cmd`;
  private cliente: MqttClient;

  constructor() {
    this.cliente = mqtt.connect(environment.mqttUrl, {
      username: environment.mqttUser,
      password: environment.mqttPass,
      clientId: `gestock-web-${Math.random().toString(16).slice(2, 10)}`,
      reconnectPeriod: 3000,
      connectTimeout: 10_000,
      clean: true,
    });

    this.cliente.on('connect', () => {
      this.estado.set('en_linea');
      this.cliente.subscribe([this.tEvento, this.tStatus], { qos: 0 });
    });
    this.cliente.on('reconnect', () => this.estado.set('reconectando'));
    this.cliente.on('close', () => {
      if (this.estado() === 'en_linea') {
        this.estado.set('desconectado');
      }
    });
    this.cliente.on('error', () => this.estado.set('desconectado'));

    this.cliente.on('message', (topic, payload) => {
      let json: unknown;
      try {
        json = JSON.parse(payload.toString());
      } catch {
        return; // payload inválido: se descarta
      }
      if (topic === this.tStatus) {
        const d = json as Partial<DeviceStatus>;
        this.ultimoEstado.set({
          device_id: d.device_id ?? 'esp32-01',
          online: !!d.online,
          last_seen: new Date().toISOString(),
          ip: d.ip,
          fw: d.fw,
        });
      } else if (topic === this.tEvento) {
        const e = json as EventoEsp;
        if (e && typeof e.evt === 'string') {
          this.eventos.next(e);
        }
      }
    });
  }

  /** Publica un comando al ESP32 y devuelve su cmd_id. */
  enviar(cmd: Record<string, string>): string {
    const cmd_id = Math.random().toString(16).slice(2, 10);
    this.cliente.publish(this.tCmd, JSON.stringify({ ...cmd, cmd_id }), { qos: 0 });
    return cmd_id;
  }

  reintentarAhora(): void {
    this.estado.set('reconectando');
    this.cliente.reconnect();
  }

  ngOnDestroy(): void {
    this.cliente.end(true);
  }
}
