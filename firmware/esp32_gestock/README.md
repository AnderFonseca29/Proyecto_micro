# GESTOCK · Firmware del ESP32 (Recepción Inteligente)

Firmware Arduino para la maqueta: **HC-SR04** (detección), **servomotor**
(clasificación), **LED verde** (indicador), **LCD I2C 16×2** (mensajes) y
**MQTT** contra EMQX Cloud.

## 1. Preparación

1. Copia la configuración y edítala:

   ```bash
   cp config.example.h config.h     # Windows: copy config.example.h config.h
   ```

   Ajusta WiFi, credenciales de EMQX y los pines/ángulos de tu maqueta.

2. Elige el entorno:

   - **PlatformIO**: abre esta carpeta y ejecuta `pio run -t upload`.
   - **Arduino IDE**: instala las librerías `PubSubClient`, `ArduinoJson`,
     `ESP32Servo` y `LiquidCrystal_I2C`, abre `esp32_gestock.ino` y sube el sketch.

## 2. Conexión física

| Componente | Pin ESP32 | Notas |
| ---------- | --------- | ----- |
| HC-SR04 VCC | 5V | |
| HC-SR04 GND | GND | |
| HC-SR04 TRIG | GPIO 5 | |
| HC-SR04 ECHO | GPIO 18 | Usa divisor de tensión si el módulo no es 3.3 V |
| Servo señal | GPIO 13 | Alimenta el servo con fuente externa de 5 V |
| Servo VCC/GND | 5V / GND | GND común con el ESP32 |
| LED verde (+) | GPIO 2 | Con resistencia de 220 Ω a GND |
| LCD I2C SDA | GPIO 21 | |
| LCD I2C SCL | GPIO 22 | |

> Alimenta el ESP32 por USB y el servo con una fuente aparte; comparten GND.

## 3. Topics

| Topic | Dirección | Payload |
| ----- | --------- | ------- |
| `gestock/recepcion/sensor` | ESP32 → backend/web | `{"evento":"caja_detectada","estado":true,"dispositivo":"ESP32_GESTOCK"}` |
| `gestock/recepcion/clasificador` | backend → ESP32 | `{"registro":"exitoso","codigo_producto":"PROD-1234","accion":"clasificar","id_operacion":"…"}` |
| `gestock/recepcion/estado` | ESP32 → backend/web | `{"estado":"clasificacion_completada","codigo_producto":"PROD-1234","id_operacion":"…"}` |

## 4. Comportamiento

- **Detección** con histéresis (`VENTANA_DETECCION_MS`) para no publicar varios
  eventos de la misma caja.
- **Anti-duplicados**: recuerda las últimas operaciones (`MAX_OPS_RECORDADAS`)
  y no vuelve a mover el servo si recibe la misma orden.
- **Clasificación**: LED verde → LCD → servo empuja → servo vuelve → LED apaga
  → publica `clasificacion_completada`.
- **Latido** cada `LATIDO_INTERVALO_MS` para que el backend marque el ESP32 en línea.

## 5. Notas de seguridad

- El sketch de demostración usa `setInsecure()` para el TLS. En producción
  carga la CA con `clienteSeguro.setCACert(...)`.
- No subas `config.h` al repositorio.
