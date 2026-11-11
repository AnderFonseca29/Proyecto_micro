import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiService } from '../../services/api.service';
import { StoreService } from '../../services/store.service';
import { ToastService } from '../../services/toast.service';
import { generarUidPrueba } from '../../shared/utilidades';
import { StandCodigo } from '../../../models/gestock.models';

type EstadoEnvio = 'inactivo' | 'enviando' | 'exito' | 'error';

/**
 * ManualControl — control total desde la web:
 * selector de stand (segmented), UID opcional (generable), botón principal
 * con estados loading/éxito/error y ripple, botones secundarios
 * (probar servo). Se deshabilita si el
 * dispositivo está offline, con tooltip explicativo.
 */
@Component({
  selector: 'app-manual-control',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [],
  templateUrl: './manual-control.html',
  styleUrl: './manual-control.scss',
})
export class ManualControl {
  private readonly api = inject(ApiService);
  private readonly toast = inject(ToastService);
  readonly store = inject(StoreService);

  /** Stand seleccionado en el segmented control. */
  readonly seleccion = signal<StandCodigo>('A');
  /** UID opcional para el comando SORT. */
  readonly uid = signal('');
  /** Estado del botón principal. */
  readonly estadoEnvio = signal<EstadoEnvio>('inactivo');
  /** Último cmd_id enviado (se muestra como confirmación). */
  readonly ultimoCmd = signal<string | null>(null);

  readonly STANDS: StandCodigo[] = ['A', 'B', 'C'];

  /** ¿Se puede operar? Requiere dispositivo online. */
  readonly habilitado = computed(() => this.store.deviceOnline());

  /** Tooltip que explica por qué está deshabilitado. */
  readonly tooltip = computed(() =>
    this.habilitado()
      ? 'Envía el comando al ESP32 por MQTT'
      : 'El ESP32 está offline. Las acciones se habilitan cuando publique su estado "online".',
  );

  /** Stand destino válido y con espacio. */
  readonly standLleno = computed(() => {
    const stand = this.store.stands().find((s) => s.codigo === this.seleccion());
    return !!stand && stand.ocupacion >= stand.capacidad;
  });

  private temporizadorEstado: ReturnType<typeof setTimeout> | null = null;

  seleccionar(stand: StandCodigo): void {
    this.seleccion.set(stand);
  }

  generarUid(): void {
    this.uid.set(generarUidPrueba());
  }

  /** Envía la clasificación manual (POST /api/clasificar → MQTT SORT). */
  async enviar(): Promise<void> {
    if (!this.habilitado() || this.estadoEnvio() === 'enviando' || this.standLleno()) {
      return;
    }
    this.estadoEnvio.set('enviando');
    try {
      const rfid = this.uid().trim() || undefined;
      const res = await firstValueFrom(this.api.clasificar(this.seleccion(), rfid));
      this.ultimoCmd.set(res.cmd_id);
      this.estadoEnvio.set('exito');
      this.toast.exito(
        `Clasificación enviada al stand ${res.stand}`,
        `UID ${res.rfid} · cmd_id ${res.cmd_id}`,
      );
      this.programarReinicioEstado();
    } catch (err) {
      this.estadoEnvio.set('error');
      this.toast.error('No se pudo enviar la clasificación', this.mensajeError(err));
      this.programarReinicioEstado();
    }
  }

  /** SERVO_TEST: barrido del servo en el hardware. */
  async probarServo(): Promise<void> {
    try {
      const res = await firstValueFrom(this.api.comando('SERVO_TEST'));
      this.toast.info('Prueba de servo enviada', `cmd_id ${res.cmd_id}`);
    } catch (err) {
      this.toast.error('No se pudo probar el servo', this.mensajeError(err));
    }
  }

  /** Vuelve al estado neutro del botón principal tras 2.5 s. */
  private programarReinicioEstado(): void {
    if (this.temporizadorEstado) {
      clearTimeout(this.temporizadorEstado);
    }
    this.temporizadorEstado = setTimeout(() => this.estadoEnvio.set('inactivo'), 2500);
  }

  private mensajeError(err: unknown): string {
    const e = err as { error?: { error?: { code?: string; message?: string } } };
    if (e?.error?.error?.code === 'STAND_LLENO') {
      return 'El stand seleccionado está lleno (STAND_LLENO).';
    }
    if (e?.error?.error?.code === 'MQTT_NO_DISPONIBLE') {
      return 'El broker MQTT no está disponible.';
    }
    return e?.error?.error?.message ?? 'Error de comunicación con el servidor.';
  }
}
