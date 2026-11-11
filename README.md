# GestockClient

This project was generated using [Angular CLI](https://github.com/angular/angular-cli) version 22.2.6.

## Secciones de la aplicación

- **Panel** (`/`): KPIs, ocupación de stands en tiempo real y auditoría de movimientos.
- **Recepción Inteligente IoT** (`/recepcion`): el sensor **HC-SR04** detecta la
  caja, la **cámara** lee su código de barras/QR, el **backend Go** valida y suma
  `+1` al stock, y la orden viaja por **MQTT** hasta el **ESP32**, que clasifica
  físicamente la caja (servo + LED verde + LCD). Todo se ve en tiempo real con el
  diseño **Beige Premium**.

## Recepción Inteligente IoT

Arquitectura, instalación, configuración de **EMQX**, cableado, endpoints,
pruebas y despliegue:

- **[`docs/recepcion-inteligente.md`](docs/recepcion-inteligente.md)** — guía completa.
- [`docs/recepcion-flujo.md`](docs/recepcion-flujo.md) — resumen del flujo.
- [`backend/README.md`](backend/README.md) — backend **Go + Beego** (REST + puente MQTT).
- [`firmware/esp32_gestock/README.md`](firmware/esp32_gestock/README.md) — firmware del ESP32.

Puesta en marcha rápida:

```bash
# 1) Backend (modo simulación si no defines MQTT_BROKER_URL)
cd backend && cp .env.example .env && go run .     # http://localhost:8080/api

# 2) Frontend
npm install && npm start                            # http://localhost:4200/recepcion
```

> Sin `MQTT_BROKER_URL` el backend arranca en **modo simulación** y emula al
> ESP32; la interfaz lo indica con el chip «Modo demostración». Las credenciales
> privilegiadas de EMQX viven solo en `backend/.env` (nunca en el frontend).

## Development server

To start a local development server, run:

```bash
ng serve
```

Once the server is running, open your browser and navigate to `http://localhost:4200/`. The application will automatically reload whenever you modify any of the source files.

## Code scaffolding

Angular CLI includes powerful code scaffolding tools. To generate a new component, run:

```bash
ng generate component component-name
```

For a complete list of available schematics (such as `components`, `directives`, or `pipes`), run:

```bash
ng generate --help
```

## Building

To build the project run:

```bash
ng build
```

This will compile your project and store the build artifacts in the `dist/` directory. By default, the production build optimizes your application for performance and speed.

## Running unit tests

To execute unit tests with the [Vitest](https://vitest.dev/) test runner, use the following command:

```bash
ng test
```

## Running end-to-end tests

For end-to-end (e2e) testing, run:

```bash
ng e2e
```

Angular CLI does not come with an end-to-end testing framework by default. You can choose one that suits your needs.

## Additional Resources

For more information on using the Angular CLI, including detailed command references, visit the [Angular CLI Overview and Command Reference](https://angular.dev/tools/cli) page.
