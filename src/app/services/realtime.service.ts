import { Injectable, OnDestroy, signal } from '@angular/core';
import { Subject } from 'rxjs';
import mqtt, { MqttClient } from 'mqtt';
import { environment } from '../../environments/environment';
import {
  DeviceStatus,
  EstadoConexion,
  EventoClasificacion,
  EventoSensor,
} from '../../models/gestock.models';

/** Evento publicado por el ESP32 en <prefijo>/evento. */
export interface EventoEsp {
  evt: 'DETECTADO' | 'CLASIFICADO' | 'TIMEOUT' | 'RECHAZADO' | 'SERVO_TEST';
  id?: string;
  talla?: 'P' | 'M' | 'G';
  stand?: 'A' | 'B' | 'C';
  origen?: 'hardware' | 'web';
}

/** Orden de clasificación que viaja del backend/web hacia el ESP32. */
export interface OrdenClasificacion {
  registro: 'exitoso';
  codigo_producto: string;
  accion: 'clasificar';
  id_operacion: string;
}

/**
 * RealtimeService — cliente MQTT (WebSocket seguro) hacia el broker.
 *
 * Publica/escucha los topics de la Recepción Inteligente:
 *  - <prefijo>/sensor       (ESP32 → web): detección del HC-SR04
 *  - <prefijo>/clasificador (web → ESP32): orden de clasificación
 *  - <prefijo>/estado       (ESP32 → web): confirmación física
 *
 * También mantiene el canal heredado del panel: <prefijo>/evento,
 * <prefijo>/status y <prefijo>/cmd. mqtt.js reconecta solo; aquí solo
 * reflejamos el estado en un Signal.
 */
@Injectable({ providedIn: 'root' })
export class RealtimeService implements OnDestroy {
  readonly estado = signal<EstadoConexion>('conectando');
  /** Último estado publicado por el ESP32 (retenido en el broker). */
  readonly ultimoEstado = signal<DeviceStatus | null>(null);

  private readonly eventos = new Subject<EventoEsp>();
  readonly eventos$ = this.eventos.asObservable();

  private readonly sensor = new Subject<EventoSensor>();
  readonly eventosSensor$ = this.sensor.asObservable();

  private readonly clasificacion = new Subject<EventoClasificacion>();
  readonly eventosClasificacion$ = this.clasificacion.asObservable();

  private readonly tEvento = `${environment.prefijo}/evento`;
  private readonly tStatus = `${environment.prefijo}/status`;
  private readonly tCmd = `${environment.prefijo}/cmd`;
  private readonly tSensor = environment.topicSensor;
  private readonly tClasificador = environment.topicClasificador;
  private readonly tEstado = environment.topicEstado;
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
      this.cliente.subscribe([this.tEvento, this.tStatus, this.tSensor, this.tEstado], {
        qos: 0,
      });
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
      } else if (topic === this.tSensor) {
        const s = json as EventoSensor;
        if (s && typeof s.evento === 'string') {
          this.sensor.next(s);
        }
      } else if (topic === this.tEstado) {
        const c = json as EventoClasificacion;
        if (c && typeof c.estado === 'string') {
          this.clasificacion.next(c);
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

  /**
   * Dispara la clasificación física en la maqueta tras un ingreso válido.
   * Publica la orden en <prefijo>/clasificador (topic que escucha el ESP32:
   * enciende el LED verde, actualiza el LCD y mueve el servo para empujar la
   * caja a su estante).
   *
   * Devuelve false si el broker no está en línea (no se puede confirmar nada).
   */
  publicarClasificacion(orden: OrdenClasificacion): boolean {
    if (this.estado() !== 'en_linea') {
      return false;
    }
    this.cliente.publish(this.tClasificador, JSON.stringify(orden), { qos: 1 });
    return true;
  }

  reintentarAhora(): void {
    this.estado.set('reconectando');
    this.cliente.reconnect();
  }

  ngOnDestroy(): void {
    this.cliente.end(true);
  }
}
