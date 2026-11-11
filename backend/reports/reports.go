// Package reports genera informes CSV verificables y los mantiene en memoria
// con caducidad para su descarga autenticada.
package reports

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gestock-iot-backend/store"
)

// Reporte generado.
type Reporte struct {
	ID        string    `json:"id"`
	Nombre    string    `json:"nombre"`
	MIME      string    `json:"mime"`
	OwnerID   string    `json:"-"`
	Creado    time.Time `json:"creado"`
	Expira    time.Time `json:"expira"`
	Contenido []byte    `json:"-"`
}

// Servicio almacena reportes temporalmente.
type Servicio struct {
	mu     sync.RWMutex
	items  map[string]*Reporte
	seq    int64
}

// Nuevo crea el servicio.
func Nuevo() *Servicio {
	s := &Servicio{items: make(map[string]*Reporte)}
	go s.limpiar()
	return s
}

// Crear guarda un reporte.
func (s *Servicio) Crear(nombre string, contenido []byte, owner string) Reporte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("RPT-%d-%04d", time.Now().Unix(), s.seq)
	r := &Reporte{
		ID: id, Nombre: nombre, MIME: "text/csv; charset=utf-8", OwnerID: owner,
		Creado: time.Now().UTC(), Expira: time.Now().Add(24 * time.Hour), Contenido: contenido,
	}
	s.items[id] = r
	return *r
}

// Obtener devuelve un reporte si pertenece al usuario o es administrador.
func (s *Servicio) Obtener(id, owner string, admin bool) (Reporte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.items[id]
	if !ok || time.Now().After(r.Expira) {
		return Reporte{}, false
	}
	if !admin && r.OwnerID != owner {
		return Reporte{}, false
	}
	return *r, true
}

func (s *Servicio) limpiar() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		for k, r := range s.items {
			if time.Now().After(r.Expira) {
				delete(s.items, k)
			}
		}
		s.mu.Unlock()
	}
}

// Generar construye un CSV para el tipo solicitado.
func Generar(tipo string, st *store.Store) (string, []byte, error) {
	tipo = strings.ToLower(strings.TrimSpace(tipo))
	var nombre string
	var cabecera []string
	var filas [][]string

	switch tipo {
	case "inventario", "resumen_inventario":
		nombre = "inventario"
		cabecera = []string{"codigo", "nombre", "categoria", "stock", "stock_minimo", "stock_inicial", "activo", "actualizado_en"}
		for _, p := range st.Productos() {
			filas = append(filas, []string{p.Codigo, p.Nombre, p.Categoria,
				itoa(p.Stock), itoa(p.StockMinimo), itoa(p.StockInicial),
				boolStr(p.Activo), p.ActualizadoEn})
		}
	case "stock_bajo", "reposicion":
		nombre = "productos_stock_bajo"
		cabecera = []string{"codigo", "nombre", "categoria", "stock", "stock_minimo", "unidades_faltantes"}
		for _, p := range st.ProductosStockBajo() {
			faltan := p.StockMinimo - p.Stock
			if faltan < 0 {
				faltan = 0
			}
			filas = append(filas, []string{p.Codigo, p.Nombre, p.Categoria,
				itoa(p.Stock), itoa(p.StockMinimo), itoa(faltan)})
		}
	case "movimientos":
		nombre = "movimientos"
		cabecera = []string{"id", "tipo", "codigo", "producto", "cantidad", "stock_anterior", "stock_nuevo", "motivo", "usuario", "rol", "fecha"}
		for _, m := range st.Movimientos("", "", 0) {
			filas = append(filas, []string{itoa64(m.ID), m.Tipo, m.CodigoProducto, m.NombreProducto,
				itoa(m.Cantidad), itoa(m.StockAnterior), itoa(m.StockNuevo), m.Motivo,
				m.Usuario, m.Rol, m.FechaRegistro})
		}
	case "recepciones":
		nombre = "recepciones"
		cabecera = []string{"id", "codigo", "producto", "cantidad", "estado", "clasificacion", "origen", "usuario", "id_operacion", "fecha_registro", "fecha_clasificacion"}
		for _, r := range st.Recepciones() {
			filas = append(filas, []string{itoa64(r.ID), r.CodigoProducto, r.NombreProducto,
				itoa(r.Cantidad), r.Estado, r.Clasificacion, r.Origen, r.Usuario,
				r.IDOperacion, r.FechaRegistro, r.FechaClasificacion})
		}
	case "incidencias", "mantenimiento":
		nombre = "incidencias"
		cabecera = []string{"id", "titulo", "descripcion", "prioridad", "estado", "dispositivo", "creada_por", "asignada_a", "fecha_creacion", "fecha_cierre", "resolucion"}
		for _, in := range st.Incidencias() {
			filas = append(filas, []string{itoa64(in.ID), in.Titulo, in.Descripcion,
				in.Prioridad, in.Estado, in.Dispositivo, in.CreadaPor, in.AsignadaA,
				in.FechaCreacion, in.FechaCierre, in.Resolucion})
		}
	case "auditoria":
		nombre = "auditoria"
		cabecera = []string{"id", "evento", "detalle", "usuario", "rol", "ip", "severidad", "fecha"}
		for _, a := range st.Auditoria(0) {
			filas = append(filas, []string{itoa64(a.ID), a.Evento, a.Detalle, a.Usuario,
				a.Rol, a.IP, a.Severidad, a.Fecha})
		}
	default:
		return "", nil, errors.New("tipo de reporte no válido")
	}

	var buf bytes.Buffer
	buf.WriteString("\uFEFF") // BOM para Excel
	buf.WriteString("# GESTOCK – Sistema Inteligente de Gestión de Inventarios\n")
	buf.WriteString("# Reporte: " + nombre + "\n")
	buf.WriteString("# Generado: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("# Fuente: backend GESTOCK (datos de la sesión actual)\n")
	buf.WriteString("# Nota: este archivo refleja los datos disponibles en el momento de la generación.\n")

	w := csv.NewWriter(&buf)
	_ = w.Write(cabecera)
	for _, f := range filas {
		_ = w.Write(f)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", nil, err
	}
	return nombre + "-" + time.Now().Format("2006-01-02") + ".csv", buf.Bytes(), nil
}

func itoa(n int) string    { return fmt.Sprintf("%d", n) }
func itoa64(n int64) string { return fmt.Sprintf("%d", n) }
func boolStr(v bool) string {
	if v {
		return "SI"
	}
	return "NO"
}
