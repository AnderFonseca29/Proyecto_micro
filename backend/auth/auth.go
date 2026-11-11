// Package auth implementa autenticación JWT (HS256), hash de contraseñas con
// bcrypt y autorización por roles/permisos (RBAC) para GESTOCK.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"gestock-iot-backend/config"
)

// Rol del usuario. Coincide con los roles existentes de GESTOCK.
type Rol string

const (
	RolAdmin      Rol = "ADMINISTRADOR"
	RolSupervisor Rol = "SUPERVISOR"
	RolOperario   Rol = "OPERARIO"
	RolTecnico    Rol = "TECNICO"
	RolAuditor    Rol = "AUDITOR"
)

// Permiso atómico comprobado en el backend.
type Permiso string

const (
	PermInventarioLeer   Permiso = "inventario.leer"
	PermInventarioEscribir Permiso = "inventario.escribir"
	PermRecepcionLeer    Permiso = "recepcion.leer"
	PermRecepcionEscribir Permiso = "recepcion.escribir"
	PermMovimientosLeer  Permiso = "movimientos.leer"
	PermAuditoriaLeer    Permiso = "auditoria.leer"
	PermUsuariosLeer     Permiso = "usuarios.leer"
	PermUsuariosGestionar Permiso = "usuarios.gestionar"
	PermIoTLeer          Permiso = "iot.leer"
	PermIoTControlar     Permiso = "iot.controlar"
	PermMantenimientoLeer Permiso = "mantenimiento.leer"
	PermMantenimientoEscribir Permiso = "mantenimiento.escribir"
	PermNotificacionesLeer Permiso = "notificaciones.leer"
	PermNotificacionesGestionar Permiso = "notificaciones.gestionar"
	PermReportesLeer     Permiso = "reportes.leer"
	PermReportesGenerar  Permiso = "reportes.generar"
	PermIAUsar           Permiso = "ia.usar"
	PermIAAdministrar    Permiso = "ia.administrar"
	PermSistemaAdmin     Permiso = "sistema.admin"
	PermTelegramConfig   Permiso = "telegram.configurar"
)

var todosLosPermisos = []Permiso{
	PermInventarioLeer, PermInventarioEscribir, PermRecepcionLeer, PermRecepcionEscribir,
	PermMovimientosLeer, PermAuditoriaLeer, PermUsuariosLeer, PermUsuariosGestionar,
	PermIoTLeer, PermIoTControlar, PermMantenimientoLeer, PermMantenimientoEscribir,
	PermNotificacionesLeer, PermNotificacionesGestionar, PermReportesLeer, PermReportesGenerar,
	PermIAUsar, PermIAAdministrar, PermSistemaAdmin, PermTelegramConfig,
}

var permisosPorRol = map[Rol][]Permiso{
	RolAdmin: todosLosPermisos,
	RolSupervisor: {
		PermInventarioLeer, PermInventarioEscribir, PermRecepcionLeer, PermRecepcionEscribir,
		PermMovimientosLeer, PermMantenimientoLeer, PermMantenimientoEscribir,
		PermNotificacionesLeer, PermReportesLeer, PermReportesGenerar, PermIAUsar,
	},
	RolOperario: {
		PermInventarioLeer, PermRecepcionLeer, PermRecepcionEscribir, PermMovimientosLeer,
		PermMantenimientoLeer, PermNotificacionesLeer, PermIAUsar,
	},
	RolTecnico: {
		PermInventarioLeer, PermIoTLeer, PermIoTControlar, PermMantenimientoLeer,
		PermMantenimientoEscribir, PermNotificacionesLeer, PermReportesLeer, PermIAUsar,
	},
	RolAuditor: {
		PermInventarioLeer, PermRecepcionLeer, PermMovimientosLeer, PermAuditoriaLeer,
		PermNotificacionesLeer, PermReportesLeer, PermIAUsar,
	},
}

// Usuario interno (nunca se serializa con el hash).
type Usuario struct {
	ID           string `json:"id"`
	Nombre       string `json:"nombre"`
	Email        string `json:"email"`
	Usuario      string `json:"usuario"`
	Rol          Rol    `json:"rol"`
	Activo       bool   `json:"activo"`
	PasswordHash string `json:"-"`
	CreadoEn     string `json:"creado_en"`
}

// Publico es la representación segura para la API.
type Publico struct {
	ID        string   `json:"id"`
	Nombre    string   `json:"nombre"`
	Email     string   `json:"email"`
	Usuario   string   `json:"usuario"`
	Rol       Rol      `json:"rol"`
	Activo    bool     `json:"activo"`
	Permisos  []Permiso `json:"permisos"`
	CreadoEn  string   `json:"creado_en"`
}

// Error de autenticación genérico (no revela si el usuario existe).
var ErrCredenciales = errors.New("usuario o contraseña incorrectos")

type intento struct {
	fallos int
	desde  time.Time
}

// Servicio mantiene usuarios y emite/valida tokens.
type Servicio struct {
	mu       sync.RWMutex
	usuarios map[string]*Usuario // por nombre de usuario (minúsculas)
	porID    map[string]*Usuario
	secreto  []byte
	ttl      time.Duration
	maxFallos int
	intentos map[string]*intento
}

// Nuevo crea el servicio y siembra las cuentas demo (configurables).
func Nuevo(cfg config.Config) *Servicio {
	s := &Servicio{
		usuarios:  make(map[string]*Usuario),
		porID:     make(map[string]*Usuario),
		secreto:   []byte(cfg.JWTSecret),
		ttl:       cfg.JWTTTL,
		maxFallos: cfg.LoginMaxIntentos,
		intentos:  make(map[string]*intento),
	}
	if s.ttl <= 0 {
		s.ttl = 8 * time.Hour
	}
	if s.maxFallos <= 0 {
		s.maxFallos = 5
	}
	if cfg.SeedDemoUsers {
		s.sembrar("USR-ADMIN", "Administrador GESTOCK", "admin@gestock.local", "admin", RolAdmin, cfg.AdminPassword)
		s.sembrar("USR-SUPER", "Supervisor de Inventario", "supervisor@gestock.local", "supervisor", RolSupervisor, cfg.SupervisorPass)
		s.sembrar("USR-OPER", "Operario de Recepción", "operario@gestock.local", "operario", RolOperario, cfg.OperarioPass)
		s.sembrar("USR-TEC", "Técnico de Mantenimiento", "tecnico@gestock.local", "tecnico", RolTecnico, cfg.TecnicoPass)
		s.sembrar("USR-AUD", "Auditor", "auditor@gestock.local", "auditor", RolAuditor, cfg.AuditorPass)
	}
	return s
}

func (s *Servicio) sembrar(id, nombre, email, usuario string, rol Rol, password string) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	u := &Usuario{
		ID: id, Nombre: nombre, Email: email, Usuario: usuario, Rol: rol,
		Activo: true, PasswordHash: string(hash), CreadoEn: time.Now().UTC().Format(time.RFC3339),
	}
	s.usuarios[strings.ToLower(usuario)] = u
	s.porID[id] = u
}

// TienePermiso comprueba el RBAC del rol.
func (u Usuario) TienePermiso(p Permiso) bool {
	if !u.Activo {
		return false
	}
	for _, permiso := range permisosPorRol[u.Rol] {
		if permiso == p {
			return true
		}
	}
	return false
}

func (u Usuario) Permisos() []Permiso {
	salida := append([]Permiso(nil), permisosPorRol[u.Rol]...)
	return salida
}

// Publico convierte el usuario a su forma segura.
func (u Usuario) Publico() Publico {
	return Publico{
		ID: u.ID, Nombre: u.Nombre, Email: u.Email, Usuario: u.Usuario,
		Rol: u.Rol, Activo: u.Activo, Permisos: u.Permisos(), CreadoEn: u.CreadoEn,
	}
}

// Autenticar valida credenciales y aplica bloqueo temporal por intentos.
// Devuelve el usuario y el token.
func (s *Servicio) Autenticar(nombre, password, ip string) (Usuario, string, time.Time, error) {
	clave := strings.ToLower(strings.TrimSpace(nombre))
	llaveIntento := clave + "|" + ip

	s.mu.Lock()
	if it, ok := s.intentos[llaveIntento]; ok {
		if it.fallos >= s.maxFallos && time.Since(it.desde) < 10*time.Minute {
			s.mu.Unlock()
			return Usuario{}, "", time.Time{}, fmt.Errorf("demasiados intentos fallidos; espera 10 minutos")
		}
		if time.Since(it.desde) > 10*time.Minute {
			delete(s.intentos, llaveIntento)
		}
	}
	u, existe := s.usuarios[clave]
	s.mu.Unlock()

	if !existe || !u.Activo {
		s.registrarFallo(llaveIntento)
		return Usuario{}, "", time.Time{}, ErrCredenciales
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.registrarFallo(llaveIntento)
		return Usuario{}, "", time.Time{}, ErrCredenciales
	}

	s.mu.Lock()
	delete(s.intentos, llaveIntento)
	copia := *u
	s.mu.Unlock()

	token, exp, err := s.Firmar(copia)
	return copia, token, exp, err
}

func (s *Servicio) registrarFallo(llave string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.intentos[llave]
	if it == nil || time.Since(it.desde) > 10*time.Minute {
		s.intentos[llave] = &intento{fallos: 1, desde: time.Now()}
		return
	}
	it.fallos++
}

// FallosRecientes devuelve los intentos fallidos de una IP/usuario (para
// generar la notificación de seguridad sin revelar la contraseña).
func (s *Servicio) FallosRecientes(nombre string, ip string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if it := s.intentos[strings.ToLower(nombre)+"|"+ip]; it != nil {
		return it.fallos
	}
	return 0
}

// Firmar emite un JWT HS256.
func (s *Servicio) Firmar(u Usuario) (string, time.Time, error) {
	ahora := time.Now().UTC()
	exp := ahora.Add(s.ttl)
	cabecera := map[string]string{"alg": "HS256", "typ": "JWT"}
	cuerpo := map[string]any{
		"sub": u.ID,
		"rol": string(u.Rol),
		"iat": ahora.Unix(),
		"exp": exp.Unix(),
		"iss": "gestock",
	}
	h, _ := json.Marshal(cabecera)
	c, _ := json.Marshal(cuerpo)
	sinFirma := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	mac := hmac.New(sha256.New, s.secreto)
	mac.Write([]byte(sinFirma))
	firma := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return sinFirma + "." + firma, exp, nil
}

// Validar verifica firma, expiración y estado del usuario.
func (s *Servicio) Validar(token string) (Usuario, error) {
	partes := strings.Split(token, ".")
	if len(partes) != 3 {
		return Usuario{}, errors.New("token inválido")
	}
	sinFirma := partes[0] + "." + partes[1]
	mac := hmac.New(sha256.New, s.secreto)
	mac.Write([]byte(sinFirma))
	esperada := mac.Sum(nil)
	recibida, err := base64.RawURLEncoding.DecodeString(partes[2])
	if err != nil || subtle.ConstantTimeCompare(esperada, recibida) != 1 {
		return Usuario{}, errors.New("firma inválida")
	}
	cuerpo, err := base64.RawURLEncoding.DecodeString(partes[1])
	if err != nil {
		return Usuario{}, errors.New("token inválido")
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
		Rol string `json:"rol"`
	}
	if err := json.Unmarshal(cuerpo, &claims); err != nil {
		return Usuario{}, errors.New("token inválido")
	}
	if time.Now().Unix() >= claims.Exp {
		return Usuario{}, errors.New("token expirado")
	}
	s.mu.RLock()
	u, ok := s.porID[claims.Sub]
	s.mu.RUnlock()
	if !ok || !u.Activo || string(u.Rol) != claims.Rol {
		return Usuario{}, errors.New("usuario no válido")
	}
	return *u, nil
}

// UsuarioPorID devuelve una copia segura.
func (s *Servicio) UsuarioPorID(id string) (Usuario, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.porID[id]
	if !ok {
		return Usuario{}, false
	}
	return *u, true
}

// Listar devuelve todos los usuarios (solo para permisos usuarios.leer).
func (s *Servicio) Listar() []Publico {
	s.mu.RLock()
	defer s.mu.RUnlock()
	salida := make([]Publico, 0, len(s.porID))
	for _, u := range s.porID {
		salida = append(salida, u.Publico())
	}
	return salida
}
