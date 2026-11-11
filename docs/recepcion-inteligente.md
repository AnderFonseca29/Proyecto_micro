# Recepción Inteligente IoT — guía completa

Módulo de **recepción inteligente** de GESTOCK: un paquete llega a la banda, el
sensor **HC-SR04** lo detecta, la **cámara** lee su código de barras/QR, el
**backend** valida el código y suma `+1` al stock, publica una orden por
**MQTT** y el **ESP32** clasifica físicamente la caja (servo + LED + LCD). Todo
se refleja **en tiempo real** en la interfaz Angular con el diseño **Beige
Premium**.

> Esta guía documenta la implementación real de este repositorio. Cuando no hay
> hardware disponible, el sistema funciona en **modo simulación** y así se
> etiqueta en la interfaz: no se afirma que se probó hardware físico si no se
> hizo.

---

## 1. Arquitectura

```
 ┌──────────┐  detecta   ┌──────────┐   MQTT    ┌────────────────┐
 │ HC-SR04  │ ─────────▶ │  ESP32   │ ────────▶ │  Broker EMQX   │
 └──────────┘            └────┬─────┘  sensor   └───────┬────────┘
                              │ ▲                        │
              clasificador     │ │  estado                │
              (orden)         │ │  (confirmación)         │
                              ▼ │                        ▼
                        ┌───────┴────────┐      ┌────────────────────┐
                        │  ESP32 servo   │      │  Backend Go/Beego  │
                        │  LED + LCD     │◀─────│  (fuente de verdad)│
                        └────────────────┘      │  valida · stock +1 │
                                                │  registra ingreso  │
                                                └─────────┬──────────┘
                                                          │ REST
                                                          ▼
                                             ┌────────────────────────┐
                                             │ Web GESTOCK (Angular)  │
                                             │ /recepcion · cámara    │
                                             └────────────────────────┘
```

**Reparto de responsabilidades**

| Componente                                 | Rol                                                               |
| ------------------------------------------ | ----------------------------------------------------------------- |
| **Frontend Angular** (`src/app/recepcion`) | Cámara, entrada manual, UI en tiempo real, respaldo local.        |
| **Backend Go + Beego** (`backend/`)        | Fuente de verdad de catálogo/stock/historial (RAM) y puente MQTT. |
| **Broker EMQX**                            | Transporte MQTT entre backend, web y ESP32.                       |
| **ESP32** (`firmware/esp32_gestock`)       | Detección HC-SR04, clasificación física y confirmación.           |

### Estrategia _backend-first_ con respaldo

1. El **backend** es la fuente de verdad: catálogo, stock e historial viven en
   su memoria RAM (sin PostgreSQL, datos temporales de demostración).
2. Si el backend **no responde**, el frontend se degrada a un **catálogo local**
   (`localStorage`) y publica la orden **directamente por MQTT** al ESP32.
3. La confirmación física (`clasificacion_completada`) siempre se refleja en
   vivo por MQTT (`gestock/recepcion/estado`). Una clasificación **solo** se
   muestra como _Completada_ cuando llega esa confirmación real (o cuando el
   backend lo indica en modo simulación, claramente etiquetado).

---

## 2. Estructura de archivos

```
Proyecto_micro/
├── src/
│   ├── app/
│   │   ├── recepcion/
│   │   │   ├── recepcion.ts        # lógica: cámara, anti-rebote, feedback
│   │   │   ├── recepcion.html      # secciones A–G (Beige Premium)
│   │   │   └── recepcion.scss      # diseño y animaciones (scoped .recepcion)
│   │   ├── services/
│   │   │   ├── recepcion.service.ts # backend-first + respaldo local + MQTT
│   │   │   └── realtime.service.ts  # cliente MQTT (sensor/estado/clasificador)
│   │   └── app.config.ts           # provideHttpClient()
│   ├── models/gestock.models.ts    # tipos del contrato JSON
│   └── environments/environment*.ts# apiUrl + topics
├── backend/                        # servicio Go + Beego (ver backend/README.md)
│   ├── .env.example
│   ├── main.go · config/ · store/ · broker/ · controllers/ · routers/
├── firmware/esp32_gestock/         # firmware Arduino (ver su README)
│   ├── esp32_gestock.ino · config.example.h · platformio.ini
└── docs/
    ├── recepcion-inteligente.md    # este documento
    └── recepcion-flujo.md          # resumen del flujo
```

---

## 3. Requisitos previos

| Herramienta                         | Versión probada |
| ----------------------------------- | --------------- |
| Node.js                             | 24.x            |
| npm                                 | 11.x            |
| Go                                  | 1.26+           |
| (Opcional) PlatformIO / Arduino IDE | para el ESP32   |
| (Opcional) Cuenta EMQX Cloud        | broker MQTT     |

---

## 4. Instalación y ejecución

### 4.1 Backend (Go + Beego)

```bash
cd backend
cp .env.example .env        # Windows: copy .env.example .env
go mod tidy
go run .                    # queda en http://localhost:8080/api
```

- **Sin `MQTT_BROKER_URL`** arranca en **modo simulación**: emula al ESP32 y
  confirma las clasificaciones automáticamente (~1.2 s). `/api/demo/estado-iot`
  lo indica con `"simulacion": true`.
- **Con `MQTT_BROKER_URL`** se conecta al broker real, se suscribe a
  `gestock/recepcion/sensor` y `gestock/recepcion/estado`, y publica en
  `gestock/recepcion/clasificador`.

> El backend registra un aviso inofensivo `init global config instance failed …
conf/app.conf` porque no usa `conf/app.conf`; el servidor arranca igual.

### 4.2 Frontend (Angular)

```bash
npm install
npm start                   # http://localhost:4200/recepcion
```

Configura los endpoints en `src/environments/environment*.ts`:

```ts
export const environment = {
  production: false,
  apiUrl: 'http://localhost:8080/api', // backend Go
  mqttUrl: 'wss://<host>:8084/mqtt', // broker (WebSocket seguro)
  mqttUser: '<usuario-restingido>',
  mqttPass: '<clave>',
  prefijo: 'gestock/recepcion',
  topicSensor: 'gestock/recepcion/sensor',
  topicClasificador: 'gestock/recepcion/clasificador',
  topicEstado: 'gestock/recepcion/estado',
};
```

### 4.3 Firmware del ESP32

Ver [`firmware/esp32_gestock/README.md`](../firmware/esp32_gestock/README.md).

```bash
cd firmware/esp32_gestock
cp config.example.h config.h    # ajusta WiFi, EMQX y pines
pio run -t upload               # o abre el .ino en Arduino IDE
```

---

## 5. Modos de operación

| Escenario                              | Backend                          | MQTT            | Qué ocurre                                                                                 |
| -------------------------------------- | -------------------------------- | --------------- | ------------------------------------------------------------------------------------------ |
| **Demostración completa (simulación)** | `go run .` sin `MQTT_BROKER_URL` | —               | El backend valida, suma stock y **emula** la confirmación del ESP32. `"simulacion": true`. |
| **Demo con hardware real**             | `go run .` con broker            | Broker + ESP32  | Cadena real: sensor → cámara → backend → MQTT → servo/LED/LCD → confirmación.              |
| **Sin backend**                        | apagado                          | Broker en línea | El frontend usa su catálogo local y publica la orden directa al ESP32 por MQTT.            |
| **Todo desconectado**                  | apagado                          | sin conexión    | Solo entrada manual; la UI marca el ingreso como _clasificación pendiente_.                |

---

## 6. Configuración de EMQX Cloud

### 6.1 Topics

| Topic                            | Dirección           | QoS | Payload                                                                                         |
| -------------------------------- | ------------------- | --- | ----------------------------------------------------------------------------------------------- |
| `gestock/recepcion/sensor`       | ESP32 → backend/web | 0   | `{"evento":"caja_detectada","estado":true,"dispositivo":"ESP32_GESTOCK"}`                       |
| `gestock/recepcion/clasificador` | backend/web → ESP32 | 1   | `{"registro":"exitoso","codigo_producto":"PROD-1234","accion":"clasificar","id_operacion":"…"}` |
| `gestock/recepcion/estado`       | ESP32 → backend/web | 0   | `{"estado":"clasificacion_completada","codigo_producto":"PROD-1234","id_operacion":"…"}`        |

### 6.2 Usuarios y ACL (mínimo privilegio)

Crea **dos** usuarios en EMQX:

1. **Backend** (privilegiado) — credenciales solo en `backend/.env`.
   - `allow` publicar en `gestock/recepcion/clasificador`
   - `allow` suscribirse a `gestock/recepcion/sensor` y `gestock/recepcion/estado`
2. **Web** (restringido) — credenciales en `environment.ts` (visibles en el
   navegador, por eso **nunca** deben ser las de administrador).
   - `allow` suscribirse a `gestock/recepcion/sensor` y `gestock/recepcion/estado`
   - `allow` publicar en `gestock/recepcion/clasificador`

Ejemplo de ACL (adaptar a tu despliegue):

```text
# usuario: backend
topic allow gestock/recepcion/clasificador  write
topic allow gestock/recepcion/sensor        read
topic allow gestock/recepcion/estado        read

# usuario: web
topic allow gestock/recepcion/sensor        read
topic allow gestock/recepcion/estado        read
topic allow gestock/recepcion/clasificador  write
```

> Habilita el listener **WebSocket seguro (WSS)** en el puerto `8084` para el
> navegador. El ESP32 puede usar TCP `8883` (TLS) o `1883`.

---

## 7. Cableado físico (ESP32)

| Componente    | Pin ESP32 | Notas                                       |
| ------------- | --------- | ------------------------------------------- |
| HC-SR04 VCC   | 5V        |                                             |
| HC-SR04 GND   | GND       |                                             |
| HC-SR04 TRIG  | GPIO 5    |                                             |
| HC-SR04 ECHO  | GPIO 18   | Divisor de tensión si el módulo no es 3.3 V |
| Servo señal   | GPIO 13   | Fuente externa de 5 V                       |
| Servo VCC/GND | 5V / GND  | **GND común** con el ESP32                  |
| LED verde (+) | GPIO 2    | Resistencia 220 Ω a GND                     |
| LCD I2C SDA   | GPIO 21   |                                             |
| LCD I2C SCL   | GPIO 22   |                                             |

> Alimenta el ESP32 por USB y el servo con una fuente aparte; comparten GND.

---

## 8. Endpoints REST

Base: `http://localhost:8080/api`.

| Método | Ruta                    | Función                                                   |
| ------ | ----------------------- | --------------------------------------------------------- |
| GET    | `/api/health`           | Estado del servicio y del broker.                         |
| GET    | `/api/demo/productos`   | Catálogo temporal.                                        |
| GET    | `/api/demo/recepciones` | Historial temporal.                                       |
| POST   | `/api/demo/recepciones` | Registrar un ingreso (valida + stock +1 + publica orden). |
| POST   | `/api/demo/sensor`      | Informar detección del HC-SR04.                           |
| GET    | `/api/demo/estado-iot`  | Estado IoT (sensor, ESP32, MQTT, servo…).                 |
| POST   | `/api/demo/reiniciar`   | Reiniciar la demostración.                                |
| POST   | `/api/demo/reintentar`  | Reenviar la orden de clasificación.                       |

**Catálogo de demostración**

| Código      | Nombre              | Stock inicial |
| ----------- | ------------------- | ------------- |
| `PROD-1234` | Teclado USB         | 10            |
| `PROD-1235` | Mouse inalámbrico   | 15            |
| `PROD-1236` | Monitor 24 pulgadas | 5             |
| `PROD-1237` | Cable HDMI          | 20            |
| `PROD-1238` | Memoria USB         | 12            |

### Ejemplos

```bash
# Registrar un ingreso
curl -X POST http://localhost:8080/api/demo/recepciones \
  -H "Content-Type: application/json" \
  -d '{"codigo_producto":"PROD-1234","cantidad":1,"id_operacion":"uuid-1","origen":"manual"}'

# Detección del sensor
curl -X POST http://localhost:8080/api/demo/sensor \
  -H "Content-Type: application/json" \
  -d '{"evento":"caja_detectada","estado":true,"dispositivo":"ESP32_GESTOCK"}'

# Estado IoT
curl http://localhost:8080/api/demo/estado-iot
```

**Códigos de estado**

| Situación                | HTTP  | `estado`                                                |
| ------------------------ | ----- | ------------------------------------------------------- |
| Ingreso válido           | `200` | `REGISTRADO`                                            |
| Producto inexistente     | `422` | `DESCONOCIDO`                                           |
| Cantidad/código inválido | `400` | `ERROR`                                                 |
| Operación duplicada      | `200` | `REGISTRADO` + `"duplicado": true` (sin duplicar stock) |

---

## 9. Seguridad

- **Las credenciales privilegiadas de EMQX viven solo en `backend/.env`**, nunca
  en el frontend. El navegador usa un usuario **restringido por ACL**.
- `.env` y `firmware/esp32_gestock/config.h` están en `.gitignore`; solo se
  versionan los `.env.example`.
- El firmware de demostración usa `setInsecure()` para el TLS; en producción
  carga la CA con `clienteSeguro.setCACert(...)`.
- La cámara requiere `https://` o `http://localhost` (`getUserMedia`).

---

## 10. Pruebas y resultados

### Backend (Go)

```bash
cd backend
go test ./...   # OK — 7 pruebas del store
go vet ./...    # OK
go build ./...  # OK
```

Resultados verificados de la lógica del store:

- Ingreso válido → stock `+1` y recepción `REGISTRADO`.
- Código repetido → `duplicado: true` y **sin** doble stock.
- Código inexistente → `ErrProductoDesconocido` (HTTP `422`).
- Histéresis del sensor → una sola detección por presencia de caja.
- `estado-iot` en simulación → la clasificación pasa a `COMPLETADA`.
- `reiniciar` → restaura catálogo, stock e historial.

Pruebas de extremo a extremo ejecutadas contra la instancia en marcha (HTTP):
health, productos, recepción, duplicado, desconocido, sensor, estado-iot y
reiniciar.

### Frontend (Angular)

```bash
npm run build    # build de producción OK
```

> No hay pruebas unitarias de frontend en el repositorio; la verificación fue de
> compilación (`ng build`) y de la página en el navegador.

### Verificación física

La cadena con hardware real (HC-SR04 → ESP32 → servo/LED/LCD) **debe validarse
en la maqueta**. En este entorno no había hardware conectado, por lo que las
pruebas se ejecutaron en **modo simulación** y así se indica en la interfaz.

---

## 11. Despliegue

1. **Backend**: compila un binario y ejecútalo detrás de un proxy.

   ```bash
   cd backend
   go build -o gestock-iot .
   ./gestock-iot           # lee variables de entorno / .env
   ```

   - Define `CORS_ORIGINS` con el dominio real del frontend.
   - Define `MQTT_BROKER_URL` (p. ej. `ssl://<host>:8883`) y las credenciales.
   - Expón el puerto `PORT` (por defecto `8080`) por HTTPS.

2. **Frontend**: build de producción y publica `dist/gestock-client`.

   ```bash
   npm run build -- --configuration production
   ```

   - Ajusta `environment.production.ts` con la URL pública del backend
     (`apiUrl`) y del broker (`mqttUrl` en `wss://`).
   - Sirve por **HTTPS** (necesario para la cámara y para `wss://`).

3. **ESP32**: graba el firmware con las credenciales y topics definitivos.

---

## 12. Solución de problemas

| Síntoma                              | Causa probable                           | Solución                                                                            |
| ------------------------------------ | ---------------------------------------- | ----------------------------------------------------------------------------------- |
| «Backend no disponible · modo local» | Backend apagado o `apiUrl` incorrecto    | Arranca `go run .` y revisa `apiUrl`.                                               |
| La cámara no abre                    | Falta HTTPS/localhost o permiso denegado | Usa `https://`/`localhost` y concede el permiso.                                    |
| Clasificación siempre «Pendiente»    | Broker/ESP32 sin conexión                | Revisa `mqttUrl`, ACL y que el ESP32 esté en línea; usa _Reintentar clasificación_. |
| El stock no sube                     | Código fuera del catálogo                | Usa uno de los códigos de demostración.                                             |
| Doble ingreso                        | Rebote del lector                        | El sistema aplica anti-rebote y `id_operacion`; el duplicado no duplica stock.      |
