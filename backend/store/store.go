// Package store es la fuente de verdad de los datos de la demostración:
// catálogo, stock, movimientos, recepciones, incidencias, auditoría y estado
// IoT. Todo vive en memoria RAM y se reinicia al arrancar el proceso.
//
// NOTA DE ARQUITECTURA: el proyecto original no tiene PostgreSQL. Esta capa
// concentra todas las lecturas/escrituras para poder sustituirla por un
// repositorio PostgreSQL sin tocar controladores, IA ni notificaciones.
package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errores de validación del flujo de recepción e inventario.
var (
	ErrCodigoVacio         = errors.New("el código del producto está vacío")
	ErrProductoDesconocido = errors.New("el producto no existe en el catálogo")
	ErrProductoInactivo    = errors.New("el producto está inactivo")
	ErrCantidadInvalida    = errors.New("la cantidad debe ser mayor que cero")
	ErrStockInsuficiente   = errors.New("stock insuficiente para la salida")
	ErrStockInvalido       = errors.New("el stock nuevo no puede ser negativo")
	ErrIncidenciaNoExiste  = errors.New("la incidencia no existe")
	ErrEstadoInvalido      = errors.New("estado de incidencia inválido")
)

const (
	maxRecepciones = 500
	maxMovimientos = 2000
	maxAuditoria   = 2000
	maxIncidencias = 500
	// ventanaDeteccion evita que una misma caja genere varios eventos de
	// sensor seguidos (histéresis del HC-SR04).
	ventanaDeteccion = 3 * time.Second
)

// Store mantiene el estado protegido por un mutex (acceso concurrente desde
// peticiones HTTP, WebSocket y cliente MQTT).
type Store struct {
	mu          sync.Mutex
	productos   []Producto
	recepciones []Recepcion
	movimientos []Movimiento
	incidencias []Incidencia
	auditoria   []Auditoria
	seq         int64
	seqMov      int64
	seqInc      int64
	seqAud      int64
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
	ahora := time.Now().UTC().Format(time.RFC3339)
	base := []Producto{
		{Codigo: "PROD-1234", Nombre: "Teclado USB", Categoria: "Periféricos", StockInicial: 10, StockMinimo: 5},
		{Codigo: "PROD-1235", Nombre: "Mouse inalámbrico", Categoria: "Periféricos", StockInicial: 15, StockMinimo: 6},
		{Codigo: "PROD-1236", Nombre: "Monitor 24 pulgadas", Categoria: "Pantallas", StockInicial: 5, StockMinimo: 6},
		{Codigo: "PROD-1237", Nombre: "Cable HDMI", Categoria: "Cables", StockInicial: 20, StockMinimo: 8},
		{Codigo: "PROD-1238", Nombre: "Memoria USB", Categoria: "Almacenamiento", StockInicial: 12, StockMinimo: 5},
	}
	for i := range base {
		base[i].Stock = base[i].StockInicial
		base[i].Activo = true
		base[i].ActualizadoEn = ahora
	}
	return base
}

func (s *Store) reiniciarLocked() {
	s.productos = catalogoDemo()
	s.recepciones = nil
	s.movimientos = nil
	s.incidencias = nil
	s.auditoria = nil
	s.seq = 0
	s.seqMov = 0
	s.seqInc = 0
	s.seqAud = 0
	s.sensor = SensorEstado{Dispositivo: "ESP32_GESTOCK"}
	s.estado = EstadoIoT{
		Sensor:        s.sensor,
		Servo:         "REPOSO",
		Clasificacion: "—",
		UltimoMensaje: "Sistema iniciado (datos en memoria)",
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

// BuscarProductos filtra por código, nombre o categoría.
func (s *Store) BuscarProductos(q string) []Producto {
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(strings.TrimSpace(q))
	salida := make([]Producto, 0)
	for _, p := range s.productos {
		if q == "" || strings.Contains(strings.ToLower(p.Codigo), q) ||
			strings.Contains(strings.ToLower(p.Nombre), q) ||
			strings.Contains(strings.ToLower(p.Categoria), q) {
			salida = append(salida, p)
		}
	}
	return salida
}

// ProductosStockBajo devuelve los productos activos en o por debajo del mínimo.
func (s *Store) ProductosStockBajo() []Producto {
	s.mu.Lock()
	defer s.mu.Unlock()
	salida := make([]Producto, 0)
	for _, p := range s.productos {
		if p.Activo && p.Stock <= p.StockMinimo {
			salida = append(salida, p)
		}
	}
	sort.SliceStable(salida, func(i, j int) bool { return salida[i].Stock < salida[j].Stock })
	return salida
}

// Recepciones devuelve el historial completo (más reciente primero).
func (s *Store) Recepciones() []Recepcion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recepcion(nil), s.recepciones...)
}

// RecepcionesFiltradas filtra por estado y código.
func (s *Store) RecepcionesFiltradas(estado, q string, limite int) []Recepcion {
	s.mu.Lock()
	defer s.mu.Unlock()
	q = strings.ToLower(strings.TrimSpace(q))
	salida := make([]Recepcion, 0)
	for _, r := range s.recepciones {
		if estado != "" && r.Estado != estado {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.CodigoProducto), q) &&
			!strings.Contains(strings.ToLower(r.NombreProducto), q) {
			continue
		}
		salida = append(salida, r)
		if limite > 0 && len(salida) >= limite {
			break
		}
	}
	return salida
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

// Movimientos devuelve el historial de movimientos (más reciente primero).
func (s *Store) Movimientos(tipo, codigo string, limite int) []Movimiento {
	s.mu.Lock()
	defer s.mu.Unlock()
	tipo = strings.ToUpper(strings.TrimSpace(tipo))
	codigo = strings.ToLower(strings.TrimSpace(codigo))
	salida := make([]Movimiento, 0)
	for _, m := range s.movimientos {
		if tipo != "" && m.Tipo != tipo {
			continue
		}
		if codigo != "" && !strings.Contains(strings.ToLower(m.CodigoProducto), codigo) &&
			!strings.Contains(strings.ToLower(m.NombreProducto), codigo) {
			continue
		}
		salida = append(salida, m)
		if limite > 0 && len(salida) >= limite {
			break
		}
	}
	return salida
}

// Incidencias devuelve las incidencias (más reciente primero).
func (s *Store) Incidencias() []Incidencia {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Incidencia(nil), s.incidencias...)
}

// Auditoria devuelve los eventos de auditoría (más reciente primero).
func (s *Store) Auditoria(limite int) []Auditoria {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limite <= 0 || limite > len(s.auditoria) {
		limite = len(s.auditoria)
	}
	return append([]Auditoria(nil), s.auditoria[:limite]...)
}

// Resumen calcula los indicadores del inventario.
func (s *Store) Resumen() ResumenInventario {
	s.mu.Lock()
	defer s.mu.Unlock()

	r := ResumenInventario{TotalProductos: len(s.productos)}
	for _, p := range s.productos {
		if p.Activo {
			r.ProductosActivos++
			r.UnidadesDisponibles += p.Stock
		}
		if p.Activo && p.Stock == 0 {
			r.ProductosAgotados++
		}
		if p.Activo && p.Stock <= p.StockMinimo {
			r.ProductosStockBajo++
		}
	}
	for _, in := range s.incidencias {
		if in.Estado != "RESUELTA" {
			r.IncidenciasAbiertas++
		}
	}

	hoy := time.Now().UTC().Format("2006-01-02")
	for _, m := range s.movimientos {
		if len(m.FechaRegistro) < 10 || m.FechaRegistro[:10] != hoy {
			continue
		}
		r.MovimientosHoy++
		switch m.Tipo {
		case MovEntrada:
			r.EntradasHoy += m.Cantidad
		case MovSalida:
			r.SalidasHoy += m.Cantidad
		}
	}
	for _, rec := range s.recepciones {
		if len(rec.FechaRegistro) >= 10 && rec.FechaRegistro[:10] == hoy {
			r.RecepcionesHoy++
		}
	}
	return r
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

// Recepcionar valida, incrementa el stock, registra el ingreso y el movimiento.
// El segundo valor de retorno indica si la operación ya existía (idempotencia).
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

	anterior := s.productos[idx].Stock
	s.productos[idx].Stock += cantidad
	s.productos[idx].ActualizadoEn = time.Now().UTC().Format(time.RFC3339)
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
		Usuario:        sol.Usuario,
		FechaRegistro:  time.Now().UTC().Format(time.RFC3339),
	}

	s.recepciones = append([]Recepcion{rec}, s.recepciones...)
	if len(s.recepciones) > maxRecepciones {
		s.recepciones = s.recepciones[:maxRecepciones]
	}

	s.registrarMovimientoLocked(MovEntrada, s.productos[idx], cantidad, anterior,
		"Recepción "+rec.IDOperacion, sol.Usuario, sol.Rol)

	s.estado.Servo = "CLASIFICANDO"
	s.estado.Clasificacion = ClasificacionPendiente
	s.estado.UltimoMensaje = "Recepción registrada; orden de clasificación enviada"
	return rec, false, nil
}

// AplicarMovimiento registra una salida o un ajuste de stock.
func (s *Store) AplicarMovimiento(sol SolicitudMovimiento, tipo string) (Producto, Movimiento, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.buscarProductoLocked(sol.CodigoProducto)
	if idx < 0 {
		return Producto{}, Movimiento{}, ErrProductoDesconocido
	}
	if !s.productos[idx].Activo {
		return Producto{}, Movimiento{}, ErrProductoInactivo
	}

	anterior := s.productos[idx].Stock
	var cantidad int

	switch tipo {
	case MovSalida:
		if sol.Cantidad <= 0 {
			return Producto{}, Movimiento{}, ErrCantidadInvalida
		}
		if sol.Cantidad > anterior {
			return Producto{}, Movimiento{}, ErrStockInsuficiente
		}
		cantidad = sol.Cantidad
		s.productos[idx].Stock -= cantidad
	case MovAjuste:
		if sol.StockNuevo == nil || *sol.StockNuevo < 0 {
			return Producto{}, Movimiento{}, ErrStockInvalido
		}
		cantidad = *sol.StockNuevo - anterior
		s.productos[idx].Stock = *sol.StockNuevo
	default:
		return Producto{}, Movimiento{}, fmt.Errorf("tipo de movimiento inválido")
	}

	s.productos[idx].ActualizadoEn = time.Now().UTC().Format(time.RFC3339)
	m := s.registrarMovimientoLocked(tipo, s.productos[idx], cantidad, anterior,
		sol.Motivo, sol.Usuario, sol.Rol)
	return s.productos[idx], m, nil
}

func (s *Store) registrarMovimientoLocked(tipo string, p Producto, cantidad, anterior int, motivo, usuario, rol string) Movimiento {
	s.seqMov++
	m := Movimiento{
		ID:             s.seqMov,
		Tipo:           tipo,
		CodigoProducto: p.Codigo,
		NombreProducto: p.Nombre,
		Cantidad:       cantidad,
		StockAnterior:  anterior,
		StockNuevo:     p.Stock,
		Motivo:         strings.TrimSpace(motivo),
		Usuario:        usuario,
		Rol:            rol,
		FechaRegistro:  time.Now().UTC().Format(time.RFC3339),
	}
	s.movimientos = append([]Movimiento{m}, s.movimientos...)
	if len(s.movimientos) > maxMovimientos {
		s.movimientos = s.movimientos[:maxMovimientos]
	}
	return m
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

// SetMQTT refleja el estado de la conexión con el broker. Devuelve el valor
// anterior para detectar transiciones.
func (s *Store) SetMQTT(conectado bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	anterior := s.estado.MQTTConectado
	s.estado.MQTTConectado = conectado
	return anterior
}

// SetESP32 actualiza la presencia del microcontrolador. Devuelve el estado
// anterior para detectar transiciones online/offline.
func (s *Store) SetESP32(online bool, lastSeen string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	anterior := s.estado.ESP32Online
	s.estado.ESP32Online = online
	if lastSeen != "" {
		s.estado.ESP32LastSeen = lastSeen
	}
	return anterior
}

// EstadoESP32 devuelve el estado y la última vez visto.
func (s *Store) EstadoESP32() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.estado.ESP32Online, s.estado.ESP32LastSeen
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

// CrearIncidencia registra una incidencia de mantenimiento.
func (s *Store) CrearIncidencia(sol SolicitudIncidencia) Incidencia {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seqInc++
	prioridad := strings.ToUpper(strings.TrimSpace(sol.Prioridad))
	if prioridad == "" {
		prioridad = "MEDIA"
	}
	in := Incidencia{
		ID:            s.seqInc,
		Titulo:        strings.TrimSpace(sol.Titulo),
		Descripcion:   strings.TrimSpace(sol.Descripcion),
		Prioridad:     prioridad,
		Estado:        "ABIERTA",
		Dispositivo:   strings.TrimSpace(sol.Dispositivo),
		CreadaPor:     sol.CreadaPor,
		AsignadaA:     strings.TrimSpace(sol.AsignadaA),
		FechaCreacion: time.Now().UTC().Format(time.RFC3339),
	}
	s.incidencias = append([]Incidencia{in}, s.incidencias...)
	if len(s.incidencias) > maxIncidencias {
		s.incidencias = s.incidencias[:maxIncidencias]
	}
	return in
}

// CambiarIncidencia actualiza estado/asignación.
func (s *Store) CambiarIncidencia(id int64, cambio CambioIncidencia) (Incidencia, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.incidencias {
		if s.incidencias[i].ID != id {
			continue
		}
		if cambio.Estado != "" {
			estado := strings.ToUpper(strings.TrimSpace(cambio.Estado))
			switch estado {
			case "ABIERTA", "EN_PROCESO", "RESUELTA":
				s.incidencias[i].Estado = estado
				if estado == "RESUELTA" {
					s.incidencias[i].FechaCierre = time.Now().UTC().Format(time.RFC3339)
					s.incidencias[i].Resolucion = strings.TrimSpace(cambio.Resolucion)
				}
			default:
				return Incidencia{}, ErrEstadoInvalido
			}
		}
		if cambio.AsignadaA != "" {
			s.incidencias[i].AsignadaA = strings.TrimSpace(cambio.AsignadaA)
		}
		if cambio.Resolucion != "" && s.incidencias[i].Estado == "RESUELTA" {
			s.incidencias[i].Resolucion = strings.TrimSpace(cambio.Resolucion)
		}
		return s.incidencias[i], nil
	}
	return Incidencia{}, ErrIncidenciaNoExiste
}

// RegistrarAuditoria añade un evento de seguridad/auditoría.
func (s *Store) RegistrarAuditoria(a Auditoria) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seqAud++
	a.ID = s.seqAud
	if a.Fecha == "" {
		a.Fecha = time.Now().UTC().Format(time.RFC3339)
	}
	if a.Severidad == "" {
		a.Severidad = "INFO"
	}
	s.auditoria = append([]Auditoria{a}, s.auditoria...)
	if len(s.auditoria) > maxAuditoria {
		s.auditoria = s.auditoria[:maxAuditoria]
	}
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
