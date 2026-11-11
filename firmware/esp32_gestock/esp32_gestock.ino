/*
 * ===========================================================================
 *  GESTOCK · Recepción Inteligente IoT — firmware del ESP32
 * ===========================================================================
 *
 *  Flujo:
 *    1. El HC-SR04 detecta una caja en la banda y se publica el evento en
 *       `gestock/recepcion/sensor`.
 *    2. La interfaz escanea el código y el backend valida/registra el ingreso.
 *    3. El backend publica la orden en `gestock/recepcion/clasificador`.
 *    4. El ESP32 clasifica físicamente la caja: enciende el LED verde,
 *       actualiza el LCD, acciona el servomotor y publica el resultado en
 *       `gestock/recepcion/estado`.
 *
 *  Requisitos (Arduino IDE / PlatformIO):
 *    - PubSubClient          (MQTT)
 *    - ArduinoJson           (parseo JSON)
 *    - ESP32Servo            (servomotor)
 *    - LiquidCrystal_I2C     (LCD 16x2 I2C)
 *
 *  Copia `config.example.h` como `config.h` y ajusta credenciales y pines.
 * ===========================================================================
 */

#include <Arduino.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <PubSubClient.h>
#include <ESP32Servo.h>
#include <Wire.h>
#include <LiquidCrystal_I2C.h>
#include <ArduinoJson.h>

#include "config.h"

// --- Objetos globales -------------------------------------------------------
WiFiClientSecure clienteSeguro;
WiFiClient clientePlano;
PubSubClient mqtt;
Servo servo;
LiquidCrystal_I2C lcd(LCD_I2C_ADDR, LCD_COLS, LCD_ROWS);

// --- Estado de la detección -------------------------------------------------
bool cajaPresente = false;
unsigned long ultimaDeteccion = 0;
unsigned long ultimaLectura = 0;
unsigned long ultimoLatido = 0;

// --- Memoria de operaciones ejecutadas (anti-duplicados) --------------------
char opsEjecutadas[MAX_OPS_RECORDADAS][48];
int numOps = 0;

// ===========================================================================
//  Utilidades
// ===========================================================================

void lcdLinea(uint8_t fila, const String &texto) {
  String t = texto;
  while (t.length() < LCD_COLS) {
    t += ' ';
  }
  lcd.setCursor(0, fila);
  lcd.print(t.substring(0, LCD_COLS));
}

float leerDistanciaCm() {
  digitalWrite(PIN_TRIG, LOW);
  delayMicroseconds(2);
  digitalWrite(PIN_TRIG, HIGH);
  delayMicroseconds(10);
  digitalWrite(PIN_TRIG, LOW);

  // 30 ms de timeout ≈ 5 m
  unsigned long duracion = pulseIn(PIN_ECHO, HIGH, 30000UL);
  if (duracion == 0) {
    return -1.0f; // sin eco (fuera de rango)
  }
  return duracion * 0.0343f / 2.0f;
}

bool opYaEjecutada(const char *id) {
  for (int i = 0; i < numOps; i++) {
    if (strcmp(opsEjecutadas[i], id) == 0) {
      return true;
    }
  }
  return false;
}

void recordarOp(const char *id) {
  if (numOps < MAX_OPS_RECORDADAS) {
    strncpy(opsEjecutadas[numOps], id, sizeof(opsEjecutadas[0]) - 1);
    numOps++;
  } else {
    // Desplaza y guarda al final (buffer circular simple).
    for (int i = 1; i < MAX_OPS_RECORDADAS; i++) {
      strncpy(opsEjecutadas[i - 1], opsEjecutadas[i], sizeof(opsEjecutadas[0]) - 1);
    }
    strncpy(opsEjecutadas[MAX_OPS_RECORDADAS - 1], id, sizeof(opsEjecutadas[0]) - 1);
  }
}

// ===========================================================================
//  Publicaciones MQTT
// ===========================================================================

void publicarSensor(bool estado) {
  StaticJsonDocument<192> doc;
  doc["evento"] = "caja_detectada";
  doc["estado"] = estado;
  doc["dispositivo"] = MQTT_CLIENT_ID;

  char buffer[192];
  size_t n = serializeJson(doc, buffer);
  mqtt.publish(TOPIC_SENSOR, (const uint8_t *)buffer, n);
  Serial.printf("[sensor] caja_detectada=%d\n", estado);
}

void publicarEstado(const char *estado, const char *codigo, const char *op,
                    const char *mensaje) {
  StaticJsonDocument<256> doc;
  doc["estado"] = estado;
  doc["codigo_producto"] = codigo;
  doc["id_operacion"] = op;
  if (mensaje != nullptr) {
    doc["mensaje"] = mensaje;
  }

  char buffer[256];
  size_t n = serializeJson(doc, buffer);
  mqtt.publish(TOPIC_ESTADO, (const uint8_t *)buffer, n);
}

// ===========================================================================
//  Clasificación física
// ===========================================================================

void clasificarCaja(const char *codigo, const char *op) {
  Serial.printf("[clasificar] %s (op %s)\n", codigo, op);

  digitalWrite(PIN_LED_VERDE, HIGH);
  lcdLinea(0, "Clasificando...");
  lcdLinea(1, String(codigo));

  servo.write(SERVO_ANGULO_EMPUJE);
  delay(SERVO_MS_MOVIMIENTO);
  servo.write(SERVO_ANGULO_REPOSO);
  delay(SERVO_MS_ESPERA);

  digitalWrite(PIN_LED_VERDE, LOW);
  lcdLinea(0, "Clasificado OK");
  lcdLinea(1, String(codigo));

  publicarEstado("clasificacion_completada", codigo, op, nullptr);
}

void alRecibirClasificacion(char *topic, byte *payload, unsigned int len) {
  StaticJsonDocument<256> doc;
  DeserializationError err = deserializeJson(doc, payload, len);
  if (err) {
    Serial.printf("[mqtt] JSON inválido en %s: %s\n", topic, err.c_str());
    return;
  }

  const char *registro = doc["registro"] | "";
  const char *accion = doc["accion"] | "";
  const char *codigo = doc["codigo_producto"] | "";
  const char *op = doc["id_operacion"] | "";

  if (strcmp(registro, "exitoso") != 0 && strcmp(accion, "clasificar") != 0) {
    Serial.println("[mqtt] orden ignorada (registro/accion no válidos)");
    return;
  }
  if (strlen(op) == 0) {
    Serial.println("[mqtt] orden ignorada (sin id_operacion)");
    return;
  }
  if (opYaEjecutada(op)) {
    Serial.printf("[mqtt] operación %s ya ejecutada, se ignora\n", op);
    return;
  }

  recordarOp(op);
  clasificarCaja(codigo, op);
}

// ===========================================================================
//  Conectividad
// ===========================================================================

void conectarWiFi() {
  if (WiFi.status() == WL_CONNECTED) {
    return;
  }
  Serial.printf("[wifi] conectando a %s", WIFI_SSID);
  WiFi.mode(WIFI_STA);
  WiFi.begin(WIFI_SSID, WIFI_PASS);
  unsigned long inicio = millis();
  while (WiFi.status() != WL_CONNECTED && millis() - inicio < 20000) {
    delay(400);
    Serial.print('.');
  }
  Serial.println();
  if (WiFi.status() == WL_CONNECTED) {
    Serial.printf("[wifi] conectado. IP: %s\n", WiFi.localIP().toString().c_str());
    lcdLinea(0, "WiFi OK");
    lcdLinea(1, WiFi.localIP().toString());
  } else {
    Serial.println("[wifi] sin conexión, se reintentará");
  }
}

void conectarMQTT() {
  if (mqtt.connected()) {
    return;
  }
  Serial.print("[mqtt] conectando… ");
  String clientId = String(MQTT_CLIENT_ID) + "-" + String((uint32_t)ESP.getEfuseMac(), HEX);
  if (mqtt.connect(clientId.c_str(), MQTT_USER, MQTT_PASS)) {
    Serial.println("OK");
    mqtt.subscribe(TOPIC_CLASIFICADOR, 1);
    lcdLinea(0, "MQTT OK");
    lcdLinea(1, "Esperando caja");
  } else {
    Serial.printf("falló (rc=%d)\n", mqtt.state());
  }
}

// ===========================================================================
//  setup / loop
// ===========================================================================

void setup() {
  Serial.begin(115200);
  delay(200);

  pinMode(PIN_TRIG, OUTPUT);
  pinMode(PIN_ECHO, INPUT);
  pinMode(PIN_LED_VERDE, OUTPUT);
  digitalWrite(PIN_LED_VERDE, LOW);

  servo.attach(PIN_SERVO);
  servo.write(SERVO_ANGULO_REPOSO);

  Wire.begin();
  lcd.init();
  lcd.backlight();
  lcdLinea(0, "GESTOCK IoT");
  lcdLinea(1, "Iniciando...");

  conectarWiFi();

  if (MQTT_USAR_TLS) {
    clienteSeguro.setInsecure(); // Demo: no valida la CA. En producción usa setCACert().
    mqtt.setClient(clienteSeguro);
  } else {
    mqtt.setClient(clientePlano);
  }
  mqtt.setServer(MQTT_HOST, MQTT_PORT);
  mqtt.setBufferSize(512);
  mqtt.setCallback(alRecibirClasificacion);

  conectarMQTT();
}

void loop() {
  conectarWiFi();
  conectarMQTT();
  mqtt.loop();

  unsigned long ahora = millis();

  // Latido periódico para que el backend sepa que el ESP32 está en línea.
  if (mqtt.connected() && ahora - ultimoLatido >= LATIDO_INTERVALO_MS) {
    ultimoLatido = ahora;
    publicarEstado("latido", "", "", nullptr);
  }

  // Lectura del HC-SR04 con histéresis.
  if (ahora - ultimaLectura >= LECTURA_INTERVALO_MS) {
    ultimaLectura = ahora;
    float distancia = leerDistanciaCm();
    bool detectado = (distancia > 0 && distancia < DISTANCIA_DETECCION_CM);

    if (detectado && (!cajaPresente || ahora - ultimaDeteccion > VENTANA_DETECCION_MS)) {
      cajaPresente = true;
      ultimaDeteccion = ahora;
      if (mqtt.connected()) {
        publicarSensor(true);
      }
      lcdLinea(0, "Caja detectada");
      lcdLinea(1, "Escanea el codigo");
    } else if (!detectado && cajaPresente) {
      cajaPresente = false;
    }
  }
}
