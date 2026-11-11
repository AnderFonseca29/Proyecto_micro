// Package store es la fuente de verdad de los datos temporales de la
// demostración: catálogo, stock, historial de recepciones y estado IoT.
// Todo vive en memoria RAM y se reinicia al arrancar el proceso.
package store

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// Errores de validación del flujo de recepción.
var (
	ErrCodigoVacio         = errors.New("el código del producto está vacío")
	ErrProductoDesconocido = errors.New("el producto no existe en el catálogo")
	ErrProductoInactivo    = errors.New("el producto está inactivo")
	ErrCantidadInvalida    = errors.New("la cantidad debe ser mayor que cero")
)

const (
	maxRecepciones = 500
	// ventanaDeteccion evita que una misma caja genere varios eventos de
	// sensor seguidos (histéresis del HC-SR04).
	ventanaDeteccion = 3 * time.Second
)

// Store mantiene el estado temporal protegido por un mutex (acceso concurrente
// desde las peticiones HTTP y desde el cliente MQTT).
type Store struct {
	mu          sync.Mutex
	productos   []Producto
	recepciones []Recepcion
	seq         int64
	sensor      SensorEstado
	estado      EstadoIoT
}

// Nuevo crea el almacén con el catálogo de demostración cargado.
func Nuevo() *Store {
	s := &Store{}
	s.reiniciarLocked()
	return s
}

func catalogoDemo() []Producto {
	base := []Producto{
		{Codigo: "PROD-1234", Nombre: "Teclado USB", StockInicial: 10},
		{Codigo: "PROD-1235", Nombre: "Mouse inalámbrico", StockInicial: 15},
		{Codigo: "PROD-1236", Nombre: "Monitor 24 pulgadas", StockInicial: 5},
		{Codigo: "PROD-1237", Nombre: "Cable HDMI", StockInicial: 20},
		{Codigo: "PROD-1238", Nombre: "Memoria USB", StockInicial: 12},
	}
	for i := range base {
		base[i].Stock = base[i].StockInicial
		base[i].Activo = true
	}
	return base
}

func (s *Store) reiniciarLocked() {
	s.productos = catalogoDemo()
	s.recepciones = nil
	s.seq = 0
	s.sensor = SensorEstado{Dispositivo: "ESP32_GESTOCK"}
	s.estado = EstadoIoT{
		Sensor:        s.sensor,
		Servo:         "REPOSO",
		Clasificacion: "—",
		UltimoMensaje: "Sistema iniciado (datos temporales en memoria)",
	}
}

// --- lecturas ---

// Productos devuelve una copia del catálogo actual.
func (s *Store) Productos() []Producto {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Producto(nil), s.productos...)
}

// ProductoPorCodigo busca un producto por su código (sin distinguir mayúsculas).
func (s *Store) ProductoPorCodigo(codigo string) (Producto, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.buscarProductoLocked(codigo); i >= 0 {
		return s.productos[i], true
	}
	return Producto{}, false
}

// Recepciones devuelve el historial completo (más reciente primero).
func (s *Store) Recepciones() []Recepcion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recepcion(nil), s.recepciones...)
}

// Pendientes devuelve las recepciones que aún esperan confirmación física.
func (s *Store) Pendientes() []Recepcion {
	s.mu.Lock()
	defer s.mu.Unlock()
	var salida []Recepcion
	for _, r := range s.recepciones {
		if r.Clasificacion == ClasificacionPendiente {
			salida = append(salida, r)
		}
	}
	return salida
}

// EstadoIoT construye el resumen de estado para la interfaz.
func (s *Store) EstadoIoT() EstadoIoT {
	s.mu.Lock()
	defer s.mu.Unlock()

	e := s.estado
	e.Sensor = s.sensor
	e.TotalProductos = len(s.productos)
	e.TotalRecepciones = len(s.recepciones)

	hoy := time.Now().UTC().Format("2006-01-02")
	for _, r := range s.recepciones {
		if r.Estado == EstadoRegistrado {
			e.Registrados++
			if len(r.FechaRegistro) >= 10 && r.FechaRegistro[:10] == hoy {
				e.UnidadesHoy += r.Cantidad
			}
		} else {
			e.Desconocidos++
		}
	}
	return e
}

// --- escrituras ---

// Recepcionar valida, incrementa el stock y registra el ingreso.
// El tercer valor de retorno indica si la operación ya existía (idempotencia).
func (s *Store) Recepcionar(sol SolicitudRecepcion) (Recepcion, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	codigo := strings.TrimSpace(sol.CodigoProducto)
	if codigo == "" {
		return Recepcion{}, false, ErrCodigoVacio
	}
	cantidad := sol.Cantidad
	if cantidad <= 0 {
		cantidad = 1
	}

	// Idempotencia: si la operación ya se registró, no se vuelve a sumar stock.
	if sol.IDOperacion != "" {
		for _, r := range s.recepciones {
			if r.IDOperacion == sol.IDOperacion {
				return r, true, nil
			}
		}
	}

	idx := s.buscarProductoLocked(codigo)
	if idx < 0 {
		return Recepcion{}, false, ErrProductoDesconocido
	}
	if !s.productos[idx].Activo {
		return Recepcion{}, false, ErrProductoInactivo
	}

	s.productos[idx].Stock += cantidad
	s.seq++
	rec := Recepcion{
		ID:             s.seq,
		CodigoProducto: s.productos[idx].Codigo,
		NombreProducto: s.productos[idx].Nombre,
		Cantidad:       cantidad,
		IDOperacion:    sol.IDOperacion,
		Estado:         EstadoRegistrado,
		Clasificacion:  ClasificacionPendiente,
		Origen:         origenODefecto(sol.Origen),
		FechaRegistro:  time.Now().UTC().Format(time.RFC3339),
	}

	s.recepciones = append([]Recepcion{rec}, s.recepciones...)
	if len(s.recepciones) > maxRecepciones {
		s.recepciones = s.recepciones[:maxRecepciones]
	}

	s.estado.Servo = "CLASIFICANDO"
	s.estado.Clasificacion = ClasificacionPendiente
	s.estado.UltimoMensaje = "Recepción registrada; orden de clasificación enviada"
	return rec, false, nil
}

// MarcarClasificacion actualiza el estado de clasificación de una operación.
func (s *Store) MarcarClasificacion(idOperacion string, completada bool, detalle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	estado := ClasificacionFallida
	if completada {
		estado = ClasificacionCompletada
	}
	for i := range s.recepciones {
		if s.recepciones[i].IDOperacion == idOperacion {
			s.recepciones[i].Clasificacion = estado
			s.recepciones[i].FechaClasificacion = time.Now().UTC().Format(time.RFC3339)
			s.estado.Servo = "REPOSO"
			s.estado.Clasificacion = estado
			if detalle != "" {
				s.estado.UltimoMensaje = detalle
			}
			return true
		}
	}
	return false
}

// MarcarClasificacionPendiente deja una recepción como pendiente (por ejemplo
// cuando la publicación MQTT falla) para poder reintentarla después.
func (s *Store) MarcarClasificacionPendiente(idOperacion, detalle string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.recepciones {
		if s.recepciones[i].IDOperacion == idOperacion {
			s.recepciones[i].Clasificacion = ClasificacionPendiente
			s.recepciones[i].FechaClasificacion = ""
			s.estado.Servo = "REPOSO"
			s.estado.Clasificacion = ClasificacionPendiente
			if detalle != "" {
				s.estado.UltimoMensaje = detalle
			}
			return true
		}
	}
	return false
}

// RecepcionPorOperacion busca una recepción por su id_operacion.
func (s *Store) RecepcionPorOperacion(idOperacion string) (Recepcion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.recepciones {
		if r.IDOperacion == idOperacion {
			return r, true
		}
	}
	return Recepcion{}, false
}

// RegistrarDeteccion procesa un evento del HC-SR04 con histéresis.
// Devuelve true si es una detección nueva (no repetida dentro de la ventana).
func (s *Store) RegistrarDeteccion(dispositivo string, presente bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dispositivo == "" {
		dispositivo = "ESP32_GESTOCK"
	}
	ahora := time.Now().UTC()
	nueva := false

	if presente {
		if ultima, err := time.Parse(time.RFC3339, s.sensor.UltimaDeteccion); err == nil {
			if ahora.Sub(ultima) < ventanaDeteccion {
				// Detección repetida de la misma caja: se ignora.
				s.sensor.CajaPresente = true
				s.sensor.Dispositivo = dispositivo
				return false
			}
		}
		s.sensor.Detecciones++
		s.sensor.UltimaDeteccion = ahora.Format(time.RFC3339)
		nueva = true
	}

	s.sensor.CajaPresente = presente
	s.sensor.Dispositivo = dispositivo
	s.estado.Sensor = s.sensor
	if presente {
		s.estado.UltimoMensaje = "Caja detectada por el sensor HC-SR04"
	}
	return nueva
}

// SetMQTT refleja el estado de la conexión con el broker.
func (s *Store) SetMQTT(conectado bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.estado.MQTTConectado = conectado
}

// SetESP32 actualiza la presencia del microcontrolador (latido/evento).
func (s *Store) SetESP32(online bool, lastSeen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.estado.ESP32Online = online
	if lastSeen != "" {
		s.estado.ESP32LastSeen = lastSeen
	}
}

// SetSimulacion marca el estado de simulación (sin hardware).
func (s *Store) SetSimulacion(activa bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.estado.Simulacion = activa
}

// Reiniciar restablece el catálogo, el stock y el historial.
func (s *Store) Reiniciar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reiniciarLocked()
}

// --- utilidades internas ---

func (s *Store) buscarProductoLocked(codigo string) int {
	c := strings.TrimSpace(codigo)
	for i := range s.productos {
		if strings.EqualFold(s.productos[i].Codigo, c) {
			return i
		}
	}
	return -1
}

func origenODefecto(origen string) string {
	switch origen {
	case "camara", "manual", "sensor", "web":
		return origen
	default:
		return "web"
	}
}
