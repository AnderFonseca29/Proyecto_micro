# GESTOCK · Recepción Inteligente IoT — Backend (Go + Beego)

Servicio independiente que sirve como **fuente de verdad de los datos temporales
de la demostración** (catálogo, stock e historial en memoria RAM) y como
**puente MQTT** entre la interfaz Angular y el ESP32.

> No usa PostgreSQL: todos los datos viven en memoria y se reinician al
> reiniciar el proceso.

## Requisitos

- Go 1.26+
- (Opcional) Un broker MQTT compatible, p. ej. **EMQX Cloud**.

## Puesta en marcha

```bash
cd backend
cp .env.example .env      # Windows: copy .env.example .env
# Edita .env con las credenciales de tu broker (o déjalo vacío: modo simulación)
go mod tidy
go run .
```

El backend queda en `http://localhost:8080/api`.

- **Sin `MQTT_BROKER_URL`** arranca en **modo simulación**: emula al ESP32 y
  confirma las clasificaciones automáticamente (útil para demostrar sin
  hardware). La respuesta de `/api/demo/estado-iot` lo indica con
  `"simulacion": true`.
- **Con `MQTT_BROKER_URL`** se conecta al broker real, se suscribe a
  `gestock/recepcion/sensor` y `gestock/recepcion/estado`, y publica en
  `gestock/recepcion/clasificador`.

## Endpoints

| Método | Ruta | Función |
| ------ | ---- | ------- |
| GET  | `/api/health` | Estado del servicio y del broker |
| GET  | `/api/demo/productos` | Catálogo temporal |
| GET  | `/api/demo/recepciones` | Historial temporal |
| POST | `/api/demo/recepciones` | Registrar un ingreso |
| POST | `/api/demo/sensor` | Informar detección del HC-SR04 |
| GET  | `/api/demo/estado-iot` | Estado IoT (sensor, ESP32, MQTT, servo…) |
| POST | `/api/demo/reiniciar` | Reiniciar la demostración |
| POST | `/api/demo/reintentar` | Reenviar la orden de clasificación |

### Ejemplos

```bash
# Registrar un ingreso
curl -X POST http://localhost:8080/api/demo/recepciones \
  -H "Content-Type: application/json" \
  -d '{"codigo_producto":"PROD-1234","cantidad":1,"id_operacion":"uuid-1"}'

# Detección del sensor
curl -X POST http://localhost:8080/api/demo/sensor \
  -H "Content-Type: application/json" \
  -d '{"evento":"caja_detectada","estado":true,"dispositivo":"ESP32_GESTOCK"}'

# Estado IoT
curl http://localhost:8080/api/demo/estado-iot
```

## Estructura

```
backend/
├── main.go              # arranque: config, store, MQTT, Beego
├── config/              # carga de variables de entorno (.env)
├── store/               # datos temporales en memoria (+ tests)
├── broker/              # cliente MQTT (paho) y modo simulación
├── controllers/         # endpoints REST
├── routers/             # rutas y CORS
└── .env.example
```

## Pruebas

```bash
go test ./...
go vet ./...
```
