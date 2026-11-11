import {
  AfterViewInit,
  ChangeDetectionStrategy,
  Component,
  OnDestroy,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { CameraDevice, Html5Qrcode, Html5QrcodeSupportedFormats } from 'html5-qrcode';
import { EmptyState } from '../shared/components/empty-state/empty-state';
import { RecepcionService } from '../services/recepcion.service';
import { RealtimeService } from '../services/realtime.service';
import { ToastService } from '../services/toast.service';
import {
  EstadoClasificacion,
  OrigenRecepcion,
  ResultadoRecepcion,
} from '../../models/gestock.models';
import { tiempoExacto, tiempoRelativo } from '../shared/utilidades';

/** Id del contenedor donde html5-qrcode inyecta el <video>. */
const ID_LECTOR = 'lector-codigos';

/** Formatos soportados: QR + los códigos de barras 1D/2D más comunes. */
const FORMATOS = [
  Html5QrcodeSupportedFormats.QR_CODE,
  Html5QrcodeSupportedFormats.CODE_128,
  Html5QrcodeSupportedFormats.CODE_39,
  Html5QrcodeSupportedFormats.CODE_93,
  Html5QrcodeSupportedFormats.EAN_13,
  Html5QrcodeSupportedFormats.EAN_8,
  Html5QrcodeSupportedFormats.UPC_A,
  Html5QrcodeSupportedFormats.UPC_E,
  Html5QrcodeSupportedFormats.ITF,
  Html5QrcodeSupportedFormats.CODABAR,
  Html5QrcodeSupportedFormats.DATA_MATRIX,
  Html5QrcodeSupportedFormats.PDF_417,
];

/**
 * Recepcion — página "Recepción inteligente".
 *
 * Flujo completo: la cámara lee el código → el backend valida y suma stock →
 * se publica la orden por MQTT → el ESP32 clasifica físicamente y confirma.
 * Toda la cadena se refleja en tiempo real (backend + MQTT).
 */
@Component({
  selector: 'app-recepcion',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [EmptyState],
  templateUrl: './recepcion.html',
  styleUrl: './recepcion.scss',
})
export class Recepcion implements AfterViewInit, OnDestroy {
  readonly serv = inject(RecepcionService);
  readonly mqtt = inject(RealtimeService);
  private readonly toast = inject(ToastService);

  readonly ID_LECTOR = ID_LECTOR;

  // ---------- estado de la cámara ----------
  readonly camaras = signal<CameraDevice[]>([]);
  readonly camaraSel = signal<string>('');
  readonly escaneando = signal(false);
  readonly iniciando = signal(false);
  readonly soportado = signal(true);
  readonly errorCamara = signal<string | null>(null);

  // ---------- estado de la recepción ----------
  readonly ultimo = signal<ResultadoRecepcion | null>(null);
  readonly entradaManual = signal('');
  readonly escaneosSesion = signal(0);
  readonly procesando = signal(false);
  /** La lectura se pausa tras detectar un código hasta pulsar "Nueva lectura". */
  readonly pausado = signal(false);

  // ---------- animaciones ----------
  readonly animarRegistro = signal(false);
  readonly mostrarExito = signal(false);

  // ---------- derivados ----------
  readonly estado = this.serv.estado;
  readonly enLinea = computed(() => this.mqtt.estado() === 'en_linea');
  readonly backendOk = this.serv.backendOk;

  /** Códigos de prueba para probar sin códigos de barras reales. */
  readonly demo = computed(() => this.serv.productos().slice(0, 5));

  /** Últimos 30 ingresos para el historial. */
  readonly recientes = computed(() => this.serv.recepciones().slice(0, 30));

  /** Última recepción pendiente de confirmación (para reintentar). */
  readonly pendiente = computed(() => this.serv.pendientes()[0] ?? null);

  /**
   * Último resultado enriquecido con el estado VIVO del historial: así el panel
   * de producto refleja la confirmación del ESP32 cuando llega (no se queda en
   * "Pendiente" tras completarse la clasificación).
   */
  readonly ultimoVivo = computed<ResultadoRecepcion | null>(() => {
    const u = this.ultimo();
    if (!u) {
      return null;
    }
    const rec = this.serv.recepciones().find((r) => r.id_operacion === u.id_operacion);
    return rec ? { ...u, clasificacion: rec.clasificacion } : u;
  });

  /** ¿Hay una caja presente en este momento? */
  readonly cajaPresente = computed(() => this.estado().sensor.caja_presente);

  /** ¿La detección fue en los últimos 4 s? (dispara el pulso del indicador). */
  readonly deteccionReciente = computed(() => Date.now() - this.serv.ultimaDeteccionMs() < 4000);

  /** Anti-rebote: evita registrar el mismo código repetido por la cámara. */
  private ultimoCodigo = '';
  private ultimoTs = 0;

  private lector: Html5Qrcode | null = null;
  private contextoAudio: AudioContext | null = null;

  constructor() {
    this.serv.iniciar();
    // Confirma visualmente la clasificación SOLO cuando el ESP32 la reporta.
    effect(() => {
      const c = this.serv.ultimaConfirmacion();
      if (c && c.estado === 'clasificacion_completada') {
        this.mostrarExito.set(true);
        setTimeout(() => this.mostrarExito.set(false), 2600);
      }
    });
  }

  // ---------- ciclo de vida ----------

  ngAfterViewInit(): void {
    void this.detectarCamaras();
  }

  ngOnDestroy(): void {
    if (this.lector?.isScanning) {
      this.lector
        .stop()
        .then(() => this.lector?.clear())
        .catch(() => {
          /* la cámara ya se había detenido */
        });
    }
  }

  // ---------- cámara ----------

  private async detectarCamaras(): Promise<void> {
    if (typeof navigator === 'undefined' || !navigator.mediaDevices?.getUserMedia) {
      this.soportado.set(false);
      this.errorCamara.set(
        'Este navegador no expone la cámara (requiere HTTPS o localhost). Usa la entrada manual.',
      );
      return;
    }
    try {
      const lista = await Html5Qrcode.getCameras();
      this.camaras.set(lista);
      if (lista.length && !this.camaraSel()) {
        const trasera = lista.find((c) => /back|rear|trasera|environment|world/i.test(c.label));
        this.camaraSel.set((trasera ?? lista[lista.length - 1]).id);
      }
      if (!lista.length) {
        this.errorCamara.set('No se detectaron cámaras. Puedes registrar códigos manualmente.');
      }
    } catch (err) {
      this.errorCamara.set(this.mensajeErrorCamara(err));
    }
  }

  async iniciar(): Promise<void> {
    if (this.escaneando() || this.iniciando()) {
      return;
    }
    this.iniciando.set(true);
    this.errorCamara.set(null);
    try {
      this.lector ??= new Html5Qrcode(ID_LECTOR, {
        verbose: false,
        formatsToSupport: FORMATOS,
      });

      const camara = this.camaraSel();
      const objetivo: string | MediaTrackConstraints = camara || {
        facingMode: 'environment',
      };

      await this.lector.start(
        objetivo,
        {
          fps: 12,
          aspectRatio: 1.6,
          qrbox: (ancho, alto) => ({
            width: Math.floor(ancho * 0.8),
            height: Math.floor(alto * 0.5),
          }),
        },
        (texto) => this.alEscanear(texto),
        () => {
          /* fotogramas sin código: se ignoran a propósito */
        },
      );
      this.escaneando.set(true);
      this.pausado.set(false);
      // Tras conceder permiso los nombres de cámara ya son legibles.
      void this.detectarCamaras();
    } catch (err) {
      const mensaje = this.mensajeErrorCamara(err);
      this.errorCamara.set(mensaje);
      this.toast.error('No se pudo iniciar la cámara', mensaje);
    } finally {
      this.iniciando.set(false);
    }
  }

  async detener(): Promise<void> {
    if (!this.lector || !this.escaneando()) {
      return;
    }
    try {
      await this.lector.stop();
      this.lector.clear();
    } catch {
      /* la cámara ya se había detenido */
    }
    this.escaneando.set(false);
  }

  async cambiarCamara(id: string): Promise<void> {
    this.camaraSel.set(id);
    if (this.escaneando()) {
      await this.detener();
      await this.iniciar();
    }
  }

  // ---------- procesamiento del código ----------

  /** Callback de la cámara con anti-rebote de 2.5 s por código repetido. */
  private alEscanear(texto: string): void {
    if (this.pausado() || this.procesando()) {
      return;
    }
    const codigo = texto.trim();
    if (!codigo) {
      return;
    }
    const ahora = Date.now();
    if (codigo === this.ultimoCodigo && ahora - this.ultimoTs < 2500) {
      return;
    }
    this.ultimoCodigo = codigo;
    this.ultimoTs = ahora;
    void this.procesar(codigo, 'camara');
  }

  /** Valida, registra el ingreso y avisa a la maqueta. */
  async procesar(codigo: string, origen: OrigenRecepcion): Promise<void> {
    if (this.procesando()) {
      return;
    }
    this.procesando.set(true);
    this.pausado.set(true); // pausa la lectura tras detectar un producto
    try {
      const resultado = await this.serv.recepcionar(codigo, origen);
      this.ultimo.set(resultado);
      this.escaneosSesion.update((n) => n + 1);
      this.retroalimentacion(resultado.ok);

      if (resultado.ok) {
        this.animarRegistro.set(true);
        setTimeout(() => this.animarRegistro.set(false), 1600);
        this.toast.exito(
          resultado.duplicado ? 'Ingreso ya registrado' : 'Ingreso registrado',
          `${resultado.codigo} · stock ${resultado.stock ?? '—'}${
            resultado.mqtt_enviado ? ' · orden enviada' : ' · clasificación pendiente'
          }`,
        );
      } else if (resultado.estado === 'DESCONOCIDO') {
        this.toast.aviso('Producto no registrado', resultado.mensaje);
      } else {
        this.toast.error('No se pudo registrar', resultado.mensaje);
      }
    } finally {
      this.procesando.set(false);
    }
  }

  /** Envía el código escrito a mano (fallback sin cámara). */
  registrarManual(): void {
    const codigo = this.entradaManual().trim();
    if (!codigo) {
      this.toast.info('Escribe un código', 'Introduce un código de barras o QR.');
      return;
    }
    this.entradaManual.set('');
    void this.procesar(codigo, 'manual');
  }

  /** Prueba rápida con un código del catálogo de demo. */
  probarDemo(codigo: string): void {
    void this.procesar(codigo, 'manual');
  }

  /** Reanuda la lectura tras una detección (nueva lectura). */
  nuevaLectura(): void {
    this.pausado.set(false);
    this.ultimoCodigo = '';
    this.entradaManual.set('');
    this.toast.info('Lectura reanudada', 'Enfoca un nuevo código o escríbelo manualmente.');
  }

  /** Limpia la lectura actual sin afectar el historial. */
  limpiarLectura(): void {
    this.ultimo.set(null);
    this.entradaManual.set('');
    this.pausado.set(false);
    this.ultimoCodigo = '';
  }

  /** Reenvía la última orden de clasificación pendiente. */
  async reintentarUltimo(): Promise<void> {
    const p = this.pendiente();
    if (!p) {
      this.toast.info('Nada que reintentar', 'No hay clasificaciones pendientes.');
      return;
    }
    const ok = await this.serv.reintentar(p.id_operacion);
    if (ok) {
      this.toast.exito('Orden reenviada', `${p.codigo_producto} · esperando confirmación física.`);
    } else {
      this.toast.error('No se pudo reenviar', 'El backend o el broker MQTT no están disponibles.');
    }
  }

  /** Reinicia la demostración completa (catálogo, stock e historial). */
  async reiniciarDemo(): Promise<void> {
    await this.serv.reiniciar();
    this.ultimo.set(null);
    this.escaneosSesion.set(0);
    this.pausado.set(false);
    this.ultimoCodigo = '';
    this.toast.info('Demostración reiniciada', 'Catálogo, stock e historial restaurados.');
  }

  // ---------- presentación ----------

  /** "hace X s" reactivo. */
  relativo(iso: string): string {
    return tiempoRelativo(iso, Date.now());
  }

  /** Marca de tiempo exacta "dd/mm/aaaa, hh:mm:ss". */
  exacto(iso: string): string {
    return tiempoExacto(iso);
  }

  claseEstado(estado: string): 'ok' | 'warn' | 'danger' {
    if (estado === 'REGISTRADO') {
      return 'ok';
    }
    if (estado === 'DESCONOCIDO') {
      return 'warn';
    }
    return 'danger';
  }

  claseClasificacion(c: EstadoClasificacion | null): 'ok' | 'warn' | 'danger' | 'apagado' {
    if (c === 'COMPLETADA') {
      return 'ok';
    }
    if (c === 'PENDIENTE') {
      return 'warn';
    }
    if (c === 'FALLIDA') {
      return 'danger';
    }
    return 'apagado';
  }

  /** Variante para el texto libre que devuelve el backend ("—", "COMPLETADA"…). */
  claseClasificacionTexto(c: string): 'ok' | 'warn' | 'danger' | 'apagado' {
    return this.claseClasificacion(c as EstadoClasificacion);
  }

  etiquetaClasificacion(c: EstadoClasificacion | null): string {
    switch (c) {
      case 'COMPLETADA':
        return 'Clasificado';
      case 'PENDIENTE':
        return 'Pendiente';
      case 'FALLIDA':
        return 'Fallido';
      default:
        return 'No aplica';
    }
  }

  origenEtiqueta(origen: OrigenRecepcion): string {
    switch (origen) {
      case 'camara':
        return 'Cámara';
      case 'manual':
        return 'Manual';
      case 'sensor':
        return 'Sensor';
      default:
        return 'Web';
    }
  }

  // ---------- utilidades internas ----------

  private retroalimentacion(ok: boolean): void {
    try {
      navigator.vibrate?.(ok ? 60 : [40, 60, 40]);
    } catch {
      /* sin soporte de vibración */
    }
    try {
      this.beep(ok);
    } catch {
      /* sin soporte de audio */
    }
  }

  private beep(ok: boolean): void {
    const Ctor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctor) {
      return;
    }
    this.contextoAudio ??= new Ctor();
    const ctx = this.contextoAudio;
    if (ctx.state === 'suspended') {
      void ctx.resume();
    }
    const osc = ctx.createOscillator();
    const gan = ctx.createGain();
    osc.type = 'sine';
    osc.frequency.value = ok ? 1180 : 320;
    gan.gain.value = 0.05;
    osc.connect(gan).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + (ok ? 0.09 : 0.22));
  }

  private mensajeErrorCamara(err: unknown): string {
    const e = err as { name?: string; message?: string };
    switch (e?.name) {
      case 'NotAllowedError':
      case 'SecurityError':
        return 'Permiso de cámara denegado. Habilítalo en el navegador e inténtalo de nuevo.';
      case 'NotFoundError':
      case 'OverconstrainedError':
        return 'No se encontró una cámara disponible en este dispositivo.';
      case 'NotReadableError':
      case 'AbortError':
        return 'La cámara está siendo usada por otra aplicación.';
      default:
        return e?.message ?? 'No se pudo acceder a la cámara.';
    }
  }
}
