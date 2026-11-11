# Recepción Inteligente — flujo resumido

> Guía completa (arquitectura, instalación, EMQX, cableado, endpoints, pruebas y
> despliegue): **[`recepcion-inteligente.md`](recepcion-inteligente.md)**.

## Flujo de extremo a extremo

```
 HC-SR04 ──detecta──▶ ESP32 ──sensor──▶ EMQX ──▶ Web/Backend
                      ▲                              │
                      │ clasificador (orden)          │ escaneo cámara
                      │ ◀────────────────────────────┤
                      │                              ▼
                      │                     Backend Go valida + stock +1
                      │                     registra el ingreso
                      │                              │
                      └──── estado (confirmación) ───┘
                       servo + LED verde + LCD
```

1. **Detección física.** El HC-SR04 detecta la caja; el ESP32 publica en
   `gestock/recepcion/sensor` (con histéresis para no repetir el evento).
2. **Escaneo.** En `/recepcion` la cámara lee el código de barras/QR
   (`html5-qrcode`) con anti-rebote; también hay entrada manual y códigos de
   prueba.
3. **Validación + stock.** El código se envía al backend Go, que comprueba que
   el producto exista, suma **+1** al stock y registra la recepción.
4. **Disparador IoT.** El backend publica en `gestock/recepcion/clasificador`:

   ```json
   {
     "registro": "exitoso",
     "codigo_producto": "PROD-1234",
     "accion": "clasificar",
     "id_operacion": "…"
   }
   ```

5. **Acción en la maqueta.** El ESP32 (que deduplica por `id_operacion`) enciende
   el LED verde, actualiza el LCD y mueve el servo para empujar la caja; luego
   publica `clasificacion_completada` en `gestock/recepcion/estado`.

## Piezas del frontend

| Archivo                                 | Responsabilidad                                                                  |
| --------------------------------------- | -------------------------------------------------------------------------------- |
| `src/app/recepcion/recepcion.ts`        | Cámara (html5-qrcode), anti-rebote, entrada manual, feedback (beep/vibración).   |
| `src/app/services/recepcion.service.ts` | Capa de datos _backend-first_: catálogo/stock/historial y respaldo local + MQTT. |
| `src/app/services/realtime.service.ts`  | Cliente MQTT: escucha `sensor`/`estado` y publica en `clasificador`.             |
| `src/environments/environment.ts`       | `apiUrl`, `mqttUrl`, `prefijo` y los tres topics.                                |
| `src/models/gestock.models.ts`          | Contrato JSON (producto, recepción, estado IoT, eventos).                        |

## Modos de operación

- **Backend en línea:** fuente de verdad (REST) + eventos MQTT en vivo.
- **Sin backend:** catálogo local (`localStorage`) y orden publicada **directo**
  por MQTT al ESP32.
- **Modo simulación** (backend sin `MQTT_BROKER_URL`): el backend emula al ESP32 y
  confirma las clasificaciones; la interfaz lo indica con `"simulacion": true`.

> La cámara solo funciona en `https://` o `http://localhost`. En una LAN por HTTP
> el navegador bloquea `getUserMedia`; usa la entrada manual.
