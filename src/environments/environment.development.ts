/**
 * Configuración de entorno (desarrollo).
 *
 * Modelo de seguridad:
 *  - El backend Go (backend/) es la fuente de verdad y guarda las credenciales
 *    PRIVILEGIADAS de EMQX en su `.env` (nunca aquí).
 *  - El navegador solo usa un usuario MQTT RESTRINGIDO por ACL: puede leer
 *    sensor/estado y publicar en el topic de clasificación. No debe ser el
 *    usuario administrador del broker.
 */
export const environment = {
  production: false,
  /** Backend Go/Beego de la demostración (REST). */
  apiUrl: 'http://localhost:8080/api',
  /** Broker MQTT por WebSocket seguro (usuario restringido por ACL). */
  mqttUrl: 'wss://d8ec416f.ala.us-east-1.emqxsl.com:8084/mqtt',
  mqttUser: 'micro',
  mqttPass: '12345678',
  /** Prefijo de los topics de la recepción inteligente. */
  prefijo: 'gestock/recepcion',
  /** Topic donde el ESP32 publica la detección del HC-SR04. */
  topicSensor: 'gestock/recepcion/sensor',
  /** Topic donde se publica la orden de clasificación tras un ingreso válido. */
  topicClasificador: 'gestock/recepcion/clasificador',
  /** Topic donde el ESP32 confirma el resultado físico. */
  topicEstado: 'gestock/recepcion/estado',
};
