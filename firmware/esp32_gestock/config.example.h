#pragma once

// ===========================================================================
//  GESTOCK · Recepción Inteligente IoT — configuración del ESP32
//  Copia este archivo como `config.h` y ajusta los valores a tu maqueta.
//  `config.h` NO se versiona (contiene credenciales).
// ===========================================================================

// --- WiFi -------------------------------------------------------------------
#define WIFI_SSID "TU_RED_WIFI"
#define WIFI_PASS "TU_CLAVE_WIFI"

// --- EMQX Cloud (MQTT) ------------------------------------------------------
#define MQTT_HOST "xxxxxxxx.ala.us-east-1.emqxsl.com"
#define MQTT_PORT 8883 // 8883 = TLS, 1883 = sin TLS
#define MQTT_USER "esp32_gestock"
#define MQTT_PASS "TU_CLAVE_MQTT"
#define MQTT_CLIENT_ID "ESP32_GESTOCK"
#define MQTT_USAR_TLS true // true → WiFiClientSecure (recomendado)

// --- Topics MQTT ------------------------------------------------------------
#define TOPIC_SENSOR "gestock/recepcion/sensor"
#define TOPIC_CLASIFICADOR "gestock/recepcion/clasificador"
#define TOPIC_ESTADO "gestock/recepcion/estado"

// --- Pines ------------------------------------------------------------------
#define PIN_TRIG 5
#define PIN_ECHO 18
#define PIN_SERVO 13
#define PIN_LED_VERDE 2

// --- LCD I2C ----------------------------------------------------------------
#define LCD_I2C_ADDR 0x27
#define LCD_COLS 16
#define LCD_ROWS 2

// --- Parámetros de la maqueta (ajustables) ----------------------------------
#define DISTANCIA_DETECCION_CM 12.0f // proximidad que marca "caja presente"
#define VENTANA_DETECCION_MS 3000    // histéresis: evita eventos repetidos
#define LECTURA_INTERVALO_MS 80      // cadencia de lectura del HC-SR04
#define LATIDO_INTERVALO_MS 20000    // frecuencia del latido de estado

#define SERVO_ANGULO_REPOSO 10
#define SERVO_ANGULO_EMPUJE 95
#define SERVO_MS_MOVIMIENTO 700
#define SERVO_MS_ESPERA 400

// --- Comportamiento ---------------------------------------------------------
#define MAX_OPS_RECORDADAS 5 // operaciones recordadas para evitar duplicados
