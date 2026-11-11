/**
 * Utilidades de formato de tiempo compartidas por la UI.
 */

/** Tiempo relativo en español: "hace 5 s", "hace 2 min", "hace 1 h". */
export function tiempoRelativo(iso: string, ahoraMs: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) {
    return '—';
  }
  const seg = Math.max(0, Math.floor((ahoraMs - t) / 1000));
  if (seg < 5) {
    return 'ahora mismo';
  }
  if (seg < 60) {
    return `hace ${seg} s`;
  }
  const min = Math.floor(seg / 60);
  if (min < 60) {
    return `hace ${min} min`;
  }
  const horas = Math.floor(min / 60);
  if (horas < 24) {
    return `hace ${horas} h`;
  }
  const dias = Math.floor(horas / 24);
  return `hace ${dias} d`;
}

/** Marca de tiempo exacta: "10/11/2026, 14:03:22". */
export function tiempoExacto(iso: string): string {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) {
    return '—';
  }
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(t.getDate())}/${p(t.getMonth() + 1)}/${t.getFullYear()} ${p(t.getHours())}:${p(t.getMinutes())}:${p(t.getSeconds())}`;
}

/** UID de prueba legible: WEB-A3F7B2C1. */
export function generarUidPrueba(): string {
  const hex = Array.from(crypto.getRandomValues(new Uint8Array(4)))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
    .toUpperCase();
  return `WEB-${hex}`;
}
