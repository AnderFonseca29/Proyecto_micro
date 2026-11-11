import { Injectable, signal } from '@angular/core';
import { environment } from '../../environments/environment';

declare global {
  interface Window {
    GESTOCK_CONFIG?: { apiUrl?: string };
  }
}

const CLAVE_API = 'gestock.apiUrl';

/**
 * Configuración en tiempo de ejecución. Permite apuntar el frontend
 * desplegado (GitHub Pages) a un backend Go distinto sin recompilar.
 *
 * Prioridad:
 *  1. URL guardada por el usuario en Configuración.
 *  2. window.GESTOCK_CONFIG.apiUrl (public/config.js).
 *  3. environment.apiUrl.
 *  4. En desarrollo, http://localhost:8080/api; en producción, /api.
 */
@Injectable({ providedIn: 'root' })
export class RuntimeConfigService {
  readonly apiUrl = signal(this.resolver());

  url(ruta: string): string {
    const base = this.apiUrl().replace(/\/+$/, '');
    const limpia = ruta.startsWith('/') ? ruta : `/${ruta}`;
    return `${base}${limpia}`;
  }

  guardar(url: string): void {
    const normalizada = url.trim().replace(/\/+$/, '');
    this.apiUrl.set(normalizada || this.porDefecto());
    try {
      if (normalizada) {
        localStorage.setItem(CLAVE_API, this.apiUrl());
      } else {
        localStorage.removeItem(CLAVE_API);
      }
    } catch {
      /* almacenamiento bloqueado */
    }
  }

  /** URL del WebSocket a partir de la API configurada. */
  wsUrl(ticket: string): string {
    const base = this.apiUrl()
      .replace(/\/api\/?$/i, '')
      .replace(/^http/i, 'ws');
    return `${base}/api/ws?ticket=${encodeURIComponent(ticket)}`;
  }

  private resolver(): string {
    try {
      const guardada = localStorage.getItem(CLAVE_API);
      if (guardada) {
        return guardada.replace(/\/+$/, '');
      }
    } catch {
      /* almacenamiento bloqueado */
    }
    const runtime = window.GESTOCK_CONFIG?.apiUrl?.trim();
    if (runtime) {
      return runtime.replace(/\/+$/, '');
    }
    const entorno = (environment.apiUrl || '').trim();
    if (entorno) {
      return entorno.replace(/\/+$/, '');
    }
    return this.porDefecto();
  }

  private porDefecto(): string {
    if (typeof location !== 'undefined' && /localhost|127\.0\.0\.1/.test(location.hostname)) {
      return 'http://localhost:8080/api';
    }
    return `${location.origin}/api`;
  }
}
