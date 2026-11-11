/**
 * Sin backend: la pagina habla con el ESP32 a traves de EMQX Cloud
 * (MQTT por WebSocket seguro). Debe coincidir con main.cpp.
 */
export const environment = {
  production: true,
  mqttUrl: 'wss://d8ec416f.ala.us-east-1.emqxsl.com:8084/mqtt',
  mqttUser: 'micro',
  mqttPass: '12345678',
  prefijo: 'gestock/recepcion',
};
