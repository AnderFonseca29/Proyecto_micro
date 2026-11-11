// Package notifications implementa el centro de notificaciones interno:
// reglas por categoría/rol, estado leído por usuario y difusión WebSocket.
package notifications

import (
	"sort"
	"strings"
	"sync"
	"time"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/realtime"
)

// Categorías del centro de notificaciones.
const (
	CatInventario    = "INVENTARIO"
	CatRecepcion     = "RECEPCION"
	CatIoT           = "IOT"
	CatMantenimiento = "MANTENIMIENTO"
	CatSeguridad     = "SEGURIDAD"
	CatAuditoria     = "AUDITORIA"
	CatIA            = "IA"
	CatSistema       = "SISTEMA"
)

// Prioridades.
const (
	PrioridadBaja    = "BAJA"
	PrioridadMedia   = "MEDIA"
	PrioridadAlta    = "ALTA"
	PrioridadCritica = "CRITICA"
)

// Notificacion es un aviso del sistema.
type Notificacion struct {
	ID        string            `json:"id"`
	Categoria string            `json:"categoria"`
	Prioridad string            `json:"prioridad"`
	Titulo    string            `json:"titulo"`
	Mensaje   string            `json:"mensaje"`
	Modulo    string            `json:"modulo"`
	Entidad   string            `json:"entidad,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Roles     []string          `json:"roles"`
	UsuarioID string            `json:"usuario_id,omitempty"`
	CreadaEn  string            `json:"creada_en"`
	Clave     string            `json:"-"`

	leidaPor     map[string]bool
	archivadaPor map[string]bool
	creada       time.Time
}

// Vista es la notificación tal como la ve un usuario concreto.
type Vista struct {
	Notificacion
	Leida     bool `json:"leida"`
	Archivada bool `json:"archivada"`
}

// Entrada crea una notificación.
type Entrada struct {
	Categoria string
	Prioridad string
	Titulo    string
	Mensaje   string
	Modulo    string
	Entidad   string
	Metadata  map[string]string
	Roles     []string
	UsuarioID string
	Clave     string
}

// EventoTelegram es el contrato mínimo con el módulo Telegram (sin ciclos).
type EventoTelegram struct {
	Categoria string
	Prioridad string
	Titulo    string
	Mensaje   string
	Modulo    string
	Entidad   string
	Roles     []string
	UsuarioID string
	Fecha     string
}

// TelegramSink evita que notifications dependa de telegram.
type TelegramSink interface {
	EnviarNotificacion(EventoTelegram)
}

// Service mantiene las notificaciones en memoria y las difunde.
type Service struct {
	mu       sync.RWMutex
	items    []*Notificacion
	seq      int64
	hub      *realtime.Hub
	telegram TelegramSink
}

// Nuevo crea el servicio.
func Nuevo(hub *realtime.Hub) *Service {
	return &Service{hub: hub}
}

// SetTelegram conecta el canal externo (opcional).
func (s *Service) SetTelegram(t TelegramSink) {
	s.mu.Lock()
	s.telegram = t
	s.mu.Unlock()
}

// RolesPorCategoria define la distribución por defecto.
func RolesPorCategoria(categoria string) []string {
	switch categoria {
	case CatInventario:
		return []string{string(auth.RolAdmin), string(auth.RolSupervisor)}
	case CatRecepcion:
		return []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolOperario)}
	case CatIoT:
		return []string{string(auth.RolAdmin), string(auth.RolTecnico)}
	case CatMantenimiento:
		return []string{string(auth.RolAdmin), string(auth.RolSupervisor), string(auth.RolTecnico)}
	case CatSeguridad:
		return []string{string(auth.RolAdmin), string(auth.RolAuditor)}
	case CatAuditoria:
		return []string{string(auth.RolAdmin), string(auth.RolAuditor)}
	case CatIA:
		return []string{string(auth.RolAdmin), string(auth.RolSupervisor)}
	case CatSistema:
		return []string{string(auth.RolAdmin)}
	default:
		return []string{string(auth.RolAdmin)}
	}
}

// Crear registra, difunde y (si aplica) envía por Telegram.
func (s *Service) Crear(in Entrada) *Notificacion {
	if in.Categoria == "" {
		in.Categoria = CatSistema
	}
	if in.Prioridad == "" {
		in.Prioridad = PrioridadMedia
	}
	if len(in.Roles) == 0 {
		in.Roles = RolesPorCategoria(in.Categoria)
	}

	s.mu.Lock()
	// Deduplicación: misma clave en los últimos 5 minutos.
	if in.Clave != "" {
		for _, n := range s.items {
			if n.Clave == in.Clave && time.Since(n.creada) < 5*time.Minute {
				s.mu.Unlock()
				return nil
			}
		}
	}
	s.seq++
	n := &Notificacion{
		ID:           "NTF-" + time.Now().UTC().Format("20060102") + "-" + pad(s.seq),
		Categoria:    in.Categoria,
		Prioridad:    in.Prioridad,
		Titulo:       in.Titulo,
		Mensaje:      in.Mensaje,
		Modulo:       in.Modulo,
		Entidad:      in.Entidad,
		Metadata:     in.Metadata,
		Roles:        in.Roles,
		UsuarioID:    in.UsuarioID,
		CreadaEn:     time.Now().UTC().Format(time.RFC3339),
		Clave:        in.Clave,
		leidaPor:     make(map[string]bool),
		archivadaPor: make(map[string]bool),
		creada:       time.Now(),
	}
	s.items = append([]*Notificacion{n}, s.items...)
	if len(s.items) > 1000 {
		s.items = s.items[:1000]
	}
	hub := s.hub
	tg := s.telegram
	s.mu.Unlock()

	if hub != nil {
		// Se difunde una vista sin estado de lectura; cada cliente recalcula
		// el suyo al consultar el contador o la lista.
		vista := n.Vista(auth.Usuario{})
		vista.Leida = false
		hub.Emitir("notification", vista, func(u auth.Usuario) bool {
			return visible(n, u)
		})
	}
	if tg != nil {
		tg.EnviarNotificacion(EventoTelegram{
			Categoria: n.Categoria, Prioridad: n.Prioridad, Titulo: n.Titulo,
			Mensaje: n.Mensaje, Modulo: n.Modulo, Entidad: n.Entidad,
			Roles: n.Roles, UsuarioID: n.UsuarioID, Fecha: n.CreadaEn,
		})
	}
	return n
}

// Listar devuelve las notificaciones visibles para el usuario.
func (s *Service) Listar(u auth.Usuario, filtro, categoria, q string) []Vista {
	s.mu.RLock()
	defer s.mu.RUnlock()

	salida := make([]Vista, 0)
	for _, n := range s.items {
		if !visible(n, u) {
			continue
		}
		v := n.Vista(u)
		if v.Archivada {
			continue
		}
		switch filtro {
		case "no_leidas":
			if v.Leida {
				continue
			}
		case "leidas":
			if !v.Leida {
				continue
			}
		case "criticas":
			if v.Prioridad != PrioridadCritica {
				continue
			}
		}
		if categoria != "" && !strings.EqualFold(categoria, v.Categoria) {
			continue
		}
		if q != "" {
			busqueda := strings.ToLower(q)
			if !strings.Contains(strings.ToLower(v.Titulo), busqueda) &&
				!strings.Contains(strings.ToLower(v.Mensaje), busqueda) {
				continue
			}
		}
		salida = append(salida, v)
	}
	return salida
}

// Contador devuelve el número de no leídas visibles.
func (s *Service) Contador(u auth.Usuario) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, n := range s.items {
		if visible(n, u) && !n.leidaPor[u.ID] && !n.archivadaPor[u.ID] {
			total++
		}
	}
	return total
}

// Obtener devuelve una notificación visible por id.
func (s *Service) Obtener(u auth.Usuario, id string) (Vista, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.items {
		if n.ID == id && visible(n, u) {
			return n.Vista(u), true
		}
	}
	return Vista{}, false
}

// MarcarLeida marca una notificación como leída para el usuario.
func (s *Service) MarcarLeida(u auth.Usuario, id string, leida bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.items {
		if n.ID == id && visible(n, u) {
			n.leidaPor[u.ID] = leida
			return true
		}
	}
	return false
}

// MarcarTodas marca todas como leídas para el usuario.
func (s *Service) MarcarTodas(u auth.Usuario) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, n := range s.items {
		if visible(n, u) && !n.leidaPor[u.ID] {
			n.leidaPor[u.ID] = true
			total++
		}
	}
	return total
}

// Archivar oculta una notificación para el usuario.
func (s *Service) Archivar(u auth.Usuario, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.items {
		if n.ID == id && visible(n, u) {
			n.archivadaPor[u.ID] = true
			return true
		}
	}
	return false
}

// Vista construye la representación por usuario.
func (n *Notificacion) Vista(u auth.Usuario) Vista {
	v := Vista{Notificacion: *n}
	// No exponer mapas internos.
	v.Notificacion.leidaPor = nil
	v.Notificacion.archivadaPor = nil
	if u.ID != "" {
		v.Leida = n.leidaPor[u.ID]
		v.Archivada = n.archivadaPor[u.ID]
	}
	return v
}

func visible(n *Notificacion, u auth.Usuario) bool {
	if u.ID == "" {
		return false
	}
	if n.UsuarioID != "" {
		return n.UsuarioID == u.ID || u.Rol == auth.RolAdmin
	}
	for _, r := range n.Roles {
		if r == string(u.Rol) {
			return true
		}
	}
	return false
}

// OrdenarPorPrioridad ordena una lista ya filtrada (críticas primero, luego fecha).
func OrdenarPorPrioridad(lista []Vista) {
	peso := map[string]int{PrioridadCritica: 0, PrioridadAlta: 1, PrioridadMedia: 2, PrioridadBaja: 3}
	sort.SliceStable(lista, func(i, j int) bool {
		if peso[lista[i].Prioridad] != peso[lista[j].Prioridad] {
			return peso[lista[i].Prioridad] < peso[lista[j].Prioridad]
		}
		return lista[i].CreadaEn > lista[j].CreadaEn
	})
}

func pad(n int64) string {
	numero := ""
	if n == 0 {
		numero = "0"
	}
	for n > 0 {
		numero = string(rune('0'+n%10)) + numero
		n /= 10
	}
	for len(numero) < 4 {
		numero = "0" + numero
	}
	return numero
}
