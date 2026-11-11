// Package telegram integra Telegram Bot API: vinculación segura por código,
// preferencias por usuario, envío individual y alertas a un grupo autorizado.
package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/config"
	"gestock-iot-backend/notifications"
)

// Errores de vinculación.
var (
	ErrNoConfigurado = errors.New("Telegram no está configurado en el servidor")
	ErrCodigoInvalido = errors.New("código de vinculación inválido o expirado")
	ErrChatYaVinculado = errors.New("este chat de Telegram ya está vinculado a otra cuenta")
	ErrNoVinculado    = errors.New("el usuario no tiene Telegram vinculado")
	ErrDemasiadosIntentos = errors.New("demasiados intentos de vinculación")
)

// Preferencias por usuario.
type Preferencias struct {
	TelegramActivo bool            `json:"telegram_activo"`
	Categorias     map[string]bool `json:"categorias"`
	SoloCriticas   bool            `json:"solo_criticas"`
}

// Vinculacion relaciona una cuenta GESTOCK con un chat de Telegram.
type Vinculacion struct {
	UsuarioID   string       `json:"usuario_id"`
	Rol         auth.Rol     `json:"rol"`
	ChatID      int64        `json:"chat_id"`
	ChatNombre  string       `json:"chat_nombre"`
	UsuarioTG   string       `json:"usuario_telegram,omitempty"`
	VinculadoEn string       `json:"vinculado_en"`
	Preferencias Preferencias `json:"preferencias"`
}

// Estado es la vista segura para el perfil del usuario.
type Estado struct {
	Configurado   bool            `json:"configurado"`
	Vinculado     bool            `json:"vinculado"`
	Chat          string          `json:"chat,omitempty"`
	UsuarioTG     string          `json:"usuario_telegram,omitempty"`
	VinculadoEn   string          `json:"vinculado_en,omitempty"`
	BotUsername   string          `json:"bot_username,omitempty"`
	Preferencias  Preferencias    `json:"preferencias"`
	Permitidas    []string        `json:"categorias_permitidas"`
	GrupoActivo   bool            `json:"grupo_activo"`
	GrupoChat     string          `json:"grupo_chat,omitempty"`
	GrupoCategorias []string      `json:"grupo_categorias,omitempty"`
}

// Entrega registra el resultado de un envío.
type Entrega struct {
	ID        int64  `json:"id"`
	ChatID    string `json:"chat_id"`
	Etiqueta  string `json:"etiqueta"`
	Estado    string `json:"estado"` // ENVIADO | ERROR | REINTENTO
	Detalle   string `json:"detalle,omitempty"`
	Fecha     string `json:"fecha"`
}

type codigo struct {
	usuarioID string
	rol       auth.Rol
	expira    time.Time
	creado    time.Time
}

type trabajo struct {
	chatID   int64
	texto    string
	etiqueta string
	intentos int
}

// Servicio encapsula el bot.
type Servicio struct {
	cfg    config.Config
	mu     sync.RWMutex
	codigos map[string]*codigo
	vinculos map[string]*Vinculacion
	porChat  map[int64]string
	intentos map[int64][]time.Time
	cola     chan trabajo
	cliente  *http.Client
	entregas []Entrega
	seqEntrega int64
	grupoChat string
	grupoCategorias map[string]bool
}

// Nuevo crea el servicio (sin iniciar worker).
func Nuevo(cfg config.Config) *Servicio {
	cats := map[string]bool{
		notifications.CatInventario: true, notifications.CatRecepcion: true,
		notifications.CatIoT: true, notifications.CatMantenimiento: true,
		notifications.CatSistema: true, notifications.CatSeguridad: true,
	}
	return &Servicio{
		cfg:      cfg,
		codigos:  make(map[string]*codigo),
		vinculos: make(map[string]*Vinculacion),
		porChat:  make(map[int64]string),
		intentos: make(map[int64][]time.Time),
		cola:     make(chan trabajo, 200),
		cliente:  &http.Client{Timeout: 15 * time.Second},
		grupoChat: cfg.TelegramGroupID,
		grupoCategorias: cats,
	}
}

// Iniciar arranca el worker de envío con reintentos.
func (s *Servicio) Iniciar() {
	for i := 0; i < 2; i++ {
		go s.worker()
	}
}

// Configurado indica si hay token.
func (s *Servicio) Configurado() bool {
	return s.cfg.TelegramEnabled && s.cfg.TelegramBotToken != ""
}

// EnviarNotificacion distribuye un evento a los chats vinculados.
func (s *Servicio) EnviarNotificacion(ev notifications.EventoTelegram) {
	if !s.Configurado() {
		return
	}
	texto := formatear(ev)

	s.mu.RLock()
	vinculos := make([]Vinculacion, 0, len(s.vinculos))
	for _, v := range s.vinculos {
		vinculos = append(vinculos, *v)
	}
	grupo := s.grupoChat
	grupoCats := make(map[string]bool, len(s.grupoCategorias))
	for k, v := range s.grupoCategorias {
		grupoCats[k] = v
	}
	s.mu.RUnlock()

	for _, v := range vinculos {
		if !v.Preferencias.TelegramActivo {
			continue
		}
		if !categoriaPermitida(v.Rol, ev.Categoria) {
			continue
		}
		if v.Preferencias.SoloCriticas && ev.Prioridad != notifications.PrioridadCritica {
			continue
		}
		if !v.Preferencias.Categorias[ev.Categoria] {
			continue
		}
		s.encolar(trabajo{chatID: v.ChatID, texto: texto, etiqueta: ev.Titulo})
	}

	if grupo != "" && ev.Prioridad == notifications.PrioridadCritica && grupoCats[ev.Categoria] {
		s.encolar(trabajo{chatID: parseChatID(grupo), texto: texto, etiqueta: "GRUPO: " + ev.Titulo})
	}
}

// Estado devuelve la situación del usuario.
func (s *Servicio) Estado(u auth.Usuario) Estado {
	s.mu.RLock()
	v, ok := s.vinculos[u.ID]
	grupo := s.grupoChat
	grupoCats := make([]string, 0)
	for c := range s.grupoCategorias {
		grupoCats = append(grupoCats, c)
	}
	s.mu.RUnlock()

	permitidas := permitidas(u.Rol)
	estado := Estado{
		Configurado: s.Configurado(), Vinculado: false,
		BotUsername: s.cfg.TelegramBotUser, Permitidas: permitidas,
		GrupoActivo: grupo != "", GrupoChat: enmascararChat(grupo),
		GrupoCategorias: grupoCats,
	}
	if !ok {
		estado.Preferencias = Preferencias{
			TelegramActivo: false, Categorias: categoriasPorDefecto(permitidas), SoloCriticas: false,
		}
		return estado
	}
	estado.Vinculado = true
	estado.Chat = enmascararChat(strconv.FormatInt(v.ChatID, 10))
	estado.UsuarioTG = v.UsuarioTG
	estado.VinculadoEn = v.VinculadoEn
	estado.Preferencias = v.Preferencias
	return estado
}

// CrearCodigo genera un código temporal de un solo uso.
func (s *Servicio) CrearCodigo(u auth.Usuario) (string, time.Time, string, error) {
	if !s.Configurado() {
		return "", time.Time{}, "", ErrNoConfigurado
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Límite: un código cada 30 s por usuario.
	for k, c := range s.codigos {
		if c.usuarioID == u.ID && time.Since(c.creado) < 30*time.Second {
			exp := c.expira
			enlace := s.enlaceBot(k)
			return k, exp, enlace, nil
		}
	}
	for k, c := range s.codigos {
		if c.usuarioID == u.ID || time.Now().After(c.expira) {
			delete(s.codigos, k)
		}
	}
	code := generarCodigo()
	exp := time.Now().Add(10 * time.Minute)
	s.codigos[code] = &codigo{usuarioID: u.ID, rol: u.Rol, expira: exp, creado: time.Now()}
	return code, exp, s.enlaceBot(code), nil
}

func (s *Servicio) enlaceBot(code string) string {
	if s.cfg.TelegramBotUser == "" {
		return ""
	}
	return "https://t.me/" + strings.TrimPrefix(s.cfg.TelegramBotUser, "@") + "?start=" + code
}

// Desvincular borra la relación del usuario.
func (s *Servicio) Desvincular(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vinculos[userID]
	if !ok {
		return false
	}
	delete(s.porChat, v.ChatID)
	delete(s.vinculos, userID)
	return true
}

// ActualizarPreferencias guarda las preferencias autorizadas.
func (s *Servicio) ActualizarPreferencias(userID string, rol auth.Rol, prefs Preferencias) (Preferencias, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.vinculos[userID]
	if !ok {
		return Preferencias{}, ErrNoVinculado
	}
	permitidas := permitidas(rol)
	for cat, activo := range prefs.Categorias {
		if activo && !contiene(permitidas, cat) {
			return Preferencias{}, fmt.Errorf("categoría no autorizada: %s", cat)
		}
	}
	if prefs.Categorias == nil {
		prefs.Categorias = categoriasPorDefecto(permitidas)
	}
	v.Preferencias = prefs
	return prefs, nil
}

// EnviarPrueba encola un mensaje de prueba al chat del usuario.
func (s *Servicio) EnviarPrueba(userID string) error {
	if !s.Configurado() {
		return ErrNoConfigurado
	}
	s.mu.RLock()
	v, ok := s.vinculos[userID]
	s.mu.RUnlock()
	if !ok {
		return ErrNoVinculado
	}
	s.encolar(trabajo{
		chatID: v.ChatID, etiqueta: "Mensaje de prueba",
		texto: "✅ <b>GESTOCK – PRUEBA DE CONEXIÓN</b>\n\nTu cuenta está vinculada correctamente.\nRecibirás aquí las alertas que tengas activadas.\n\n<i>GESTOCK Notificaciones</i>",
	})
	return nil
}

// Vincular procesa /start con código.
func (s *Servicio) Vincular(chatID int64, chatNombre, usuarioTG, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Protección contra fuerza bruta por chat.
	ahora := time.Now()
	hist := s.intentos[chatID]
	nuevos := make([]time.Time, 0, len(hist)+1)
	for _, t := range hist {
		if ahora.Sub(t) < 10*time.Minute {
			nuevos = append(nuevos, t)
		}
	}
	if len(nuevos) >= 6 {
		return ErrDemasiadosIntentos
	}
	s.intentos[chatID] = append(nuevos, ahora)

	c, ok := s.codigos[strings.ToUpper(strings.TrimSpace(code))]
	if !ok || ahora.After(c.expira) {
		delete(s.codigos, strings.ToUpper(strings.TrimSpace(code)))
		return ErrCodigoInvalido
	}
	if otro, existe := s.porChat[chatID]; existe && otro != c.usuarioID {
		return ErrChatYaVinculado
	}
	// Un usuario solo puede tener un chat: se reemplaza el anterior.
	if anterior, existe := s.vinculos[c.usuarioID]; existe && anterior.ChatID != chatID {
		delete(s.porChat, anterior.ChatID)
	}
	delete(s.codigos, strings.ToUpper(strings.TrimSpace(code)))

	s.vinculos[c.usuarioID] = &Vinculacion{
		UsuarioID: c.usuarioID, Rol: c.rol, ChatID: chatID, ChatNombre: chatNombre,
		UsuarioTG: usuarioTG, VinculadoEn: ahora.UTC().Format(time.RFC3339),
		Preferencias: Preferencias{
			TelegramActivo: true,
			Categorias:     categoriasPorDefecto(permitidas(c.rol)),
			SoloCriticas:   false,
		},
	}
	s.porChat[chatID] = c.usuarioID
	return nil
}

// DesvincularPorChat procesa /stop.
func (s *Servicio) DesvincularPorChat(chatID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID, ok := s.porChat[chatID]
	if !ok {
		return false
	}
	delete(s.porChat, chatID)
	delete(s.vinculos, userID)
	return true
}

// ConfigurarGrupo valida y guarda el grupo autorizado (solo administrador).
func (s *Servicio) ConfigurarGrupo(chatID string, categorias []string) error {
	if !s.Configurado() {
		return ErrNoConfigurado
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		s.mu.Lock()
		s.grupoChat = ""
		s.mu.Unlock()
		return nil
	}
	if !strings.HasPrefix(chatID, "-") {
		return errors.New("el id de un grupo de Telegram debe empezar por '-'")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.getChat(ctx, chatID); err != nil {
		return fmt.Errorf("no se pudo validar el grupo: %w", err)
	}
	cats := make(map[string]bool)
	for _, c := range categorias {
		if c != "" {
			cats[c] = true
		}
	}
	if len(cats) == 0 {
		cats = map[string]bool{notifications.CatIoT: true, notifications.CatSistema: true}
	}
	s.mu.Lock()
	s.grupoChat = chatID
	s.grupoCategorias = cats
	s.mu.Unlock()
	return nil
}

// Entregas devuelve el registro reciente de envíos.
func (s *Servicio) Entregas() []Entrega {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Entrega(nil), s.entregas...)
}

// RegistrarWebhook configura el webhook HTTPS en Telegram con el secreto.
// Devuelve error si falta la URL o el secreto.
func (s *Servicio) RegistrarWebhook() error {
	if !s.Configurado() {
		return ErrNoConfigurado
	}
	if strings.TrimSpace(s.cfg.TelegramWebhookURL) == "" {
		return errors.New("TELEGRAM_WEBHOOK_URL no está configurada")
	}
	if strings.TrimSpace(s.cfg.TelegramWebhookSec) == "" {
		return errors.New("TELEGRAM_WEBHOOK_SECRET es obligatorio para validar el webhook")
	}
	cuerpo, _ := json.Marshal(map[string]any{
		"url":                  s.cfg.TelegramWebhookURL,
		"secret_token":         s.cfg.TelegramWebhookSec,
		"allowed_updates":      []string{"message"},
		"drop_pending_updates": true,
	})
	url := "https://api.telegram.org/bot" + s.cfg.TelegramBotToken + "/setWebhook"
	resp, err := s.cliente.Post(url, "application/json", bytes.NewReader(cuerpo))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&r)
	if !r.OK {
		return errors.New("Telegram rechazó el webhook: " + r.Description)
	}
	return nil
}

// EliminarWebhook desactiva la recepción de actualizaciones.
func (s *Servicio) EliminarWebhook() error {
	if !s.Configurado() {
		return ErrNoConfigurado
	}
	url := "https://api.telegram.org/bot" + s.cfg.TelegramBotToken + "/deleteWebhook"
	resp, err := s.cliente.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Grupo devuelve la configuración actual del grupo.
func (s *Servicio) Grupo() (string, []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cats := make([]string, 0, len(s.grupoCategorias))
	for c := range s.grupoCategorias {
		cats = append(cats, c)
	}
	return enmascararChat(s.grupoChat), cats
}

// --- worker / HTTP ---

func (s *Servicio) encolar(t trabajo) {
	select {
	case s.cola <- t:
	default:
		log.Printf("[telegram] cola llena; se descarta: %s", t.etiqueta)
	}
}

func (s *Servicio) worker() {
	for t := range s.cola {
		err := s.enviarDirecto(t.chatID, t.texto)
		if err == nil {
			s.registrarEntrega(t, "ENVIADO", "")
			continue
		}
		t.intentos++
		if t.intentos < 3 {
			s.registrarEntrega(t, "REINTENTO", err.Error())
			time.AfterFunc(time.Duration(t.intentos)*3*time.Second, func() { s.encolar(t) })
			continue
		}
		s.registrarEntrega(t, "ERROR", err.Error())
	}
}

func (s *Servicio) enviarDirecto(chatID int64, texto string) error {
	if !s.Configurado() {
		return ErrNoConfigurado
	}
	cuerpo := map[string]any{
		"chat_id": chatID, "text": texto, "parse_mode": "HTML",
		"disable_web_page_preview": true,
	}
	datos, _ := json.Marshal(cuerpo)
	url := "https://api.telegram.org/bot" + s.cfg.TelegramBotToken + "/sendMessage"
	resp, err := s.cliente.Post(url, "application/json", bytes.NewReader(datos))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal(body, &r)
	if !r.OK {
		if r.Parameters.RetryAfter > 0 {
			time.Sleep(time.Duration(r.Parameters.RetryAfter) * time.Second)
		}
		return fmt.Errorf("Telegram respondió: %s", r.Description)
	}
	return nil
}

func (s *Servicio) getChat(ctx context.Context, chatID string) error {
	url := "https://api.telegram.org/bot" + s.cfg.TelegramBotToken + "/getChat?chat_id=" + chatID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := s.cliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&r)
	if !r.OK {
		return errors.New(r.Description)
	}
	return nil
}

func (s *Servicio) registrarEntrega(t trabajo, estado, detalle string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seqEntrega++
	s.entregas = append([]Entrega{{
		ID: s.seqEntrega, ChatID: enmascararChat(strconv.FormatInt(t.chatID, 10)),
		Etiqueta: t.etiqueta, Estado: estado, Detalle: detalle,
		Fecha: time.Now().UTC().Format(time.RFC3339),
	}}, s.entregas...)
	if len(s.entregas) > 300 {
		s.entregas = s.entregas[:300]
	}
}

// --- webhook ---

// Update mínimo de Telegram.
type Update struct {
	Message *struct {
		MessageID int64 `json:"message_id"`
		From      struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID    int64  `json:"id"`
			Type  string `json:"type"`
			Title string `json:"title"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// ProcesarWebhook atiende /start, /stop, /estado y /ayuda.
func (s *Servicio) ProcesarWebhook(update Update) (string, error) {
	if update.Message == nil || update.Message.Text == "" {
		return "", nil
	}
	m := update.Message
	texto := strings.TrimSpace(m.Text)
	chatID := m.Chat.ID
	nombre := m.From.FirstName
	if m.Chat.Title != "" {
		nombre = m.Chat.Title
	}

	if strings.HasPrefix(texto, "/start") {
		partes := strings.Fields(texto)
		if len(partes) < 2 {
			s.responder(chatID, "👋 <b>GESTOCK Notificaciones</b>\n\nAbre la opción <b>Conectar Telegram</b> en GESTOCK y pulsa el enlace generado, o envía:\n\n<code>/start CÓDIGO</code>\n\nEl código caduca en 10 minutos.")
			return "instrucciones", nil
		}
		if err := s.Vincular(chatID, nombre, m.From.Username, partes[1]); err != nil {
			s.responder(chatID, "⚠️ No se pudo vincular la cuenta: "+err.Error()+"\n\nGenera un código nuevo desde GESTOCK.")
			return "error", err
		}
		s.responder(chatID, "✅ <b>GESTOCK – TELEGRAM CONECTADO</b>\n\nTu cuenta quedó vinculada. Recibirás aquí las alertas autorizadas según tus preferencias.\n\nUsa /estado para consultar la conexión o /stop para desvincular.")
		return "vinculado", nil
	}
	if strings.HasPrefix(texto, "/stop") {
		if s.DesvincularPorChat(chatID) {
			s.responder(chatID, "🔌 Tu cuenta fue desvinculada de GESTOCK. Puedes volver a conectarla cuando quieras.")
		} else {
			s.responder(chatID, "ℹ️ Este chat no está vinculado a GESTOCK.")
		}
		return "desvinculado", nil
	}
	if strings.HasPrefix(texto, "/estado") {
		s.mu.RLock()
		userID, ok := s.porChat[chatID]
		var v Vinculacion
		if ok {
			v = *s.vinculos[userID]
		}
		s.mu.RUnlock()
		if !ok {
			s.responder(chatID, "ℹ️ Este chat no está vinculado. Genera un código en GESTOCK.")
		} else {
			estado := "activadas"
			if !v.Preferencias.TelegramActivo {
				estado = "desactivadas"
			}
			s.responder(chatID, fmt.Sprintf("✅ <b>GESTOCK</b>\n\nCuenta vinculada: <b>%s</b>\nNotificaciones: <b>%s</b>\nVinculado: %s", html.EscapeString(v.ChatNombre), estado, v.VinculadoEn))
		}
		return "estado", nil
	}
	if strings.HasPrefix(texto, "/ayuda") || strings.HasPrefix(texto, "/help") {
		s.responder(chatID, "ℹ️ <b>Comandos</b>\n\n/start CÓDIGO – vincular cuenta\n/estado – ver conexión\n/stop – desvincular\n/ayuda – esta ayuda")
		return "ayuda", nil
	}
	s.responder(chatID, "ℹ️ Comando no reconocido. Usa /ayuda.")
	return "desconocido", nil
}

func (s *Servicio) responder(chatID int64, texto string) {
	s.encolar(trabajo{chatID: chatID, texto: texto, etiqueta: "Respuesta del bot"})
}

// --- formato / utilidades ---

func formatear(ev notifications.EventoTelegram) string {
	emoji := "🔔"
	switch ev.Prioridad {
	case notifications.PrioridadCritica:
		emoji = "🚨"
	case notifications.PrioridadAlta:
		emoji = "⚠️"
	case notifications.PrioridadBaja:
		emoji = "ℹ️"
	}
	categoria := strings.ReplaceAll(ev.Categoria, "_", " ")
	titulo := strings.ToUpper(ev.Titulo)
	lineas := []string{
		fmt.Sprintf("%s <b>GESTOCK – %s</b>", emoji, html.EscapeString(strings.ToUpper(categoria))),
		"",
		fmt.Sprintf("<b>%s</b>", html.EscapeString(titulo)),
		html.EscapeString(ev.Mensaje),
	}
	if ev.Modulo != "" {
		lineas = append(lineas, "", "<b>Módulo:</b> "+html.EscapeString(ev.Modulo))
	}
	if ev.Entidad != "" {
		lineas = append(lineas, "<b>Referencia:</b> "+html.EscapeString(ev.Entidad))
	}
	lineas = append(lineas, "<b>Fecha:</b> "+fechaHumana(ev.Fecha))
	lineas = append(lineas, "", "<i>GESTOCK Notificaciones</i>")
	return strings.Join(lineas, "\n")
}

func fechaHumana(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return time.Now().Format("02/01/2006 15:04")
	}
	return t.Local().Format("02/01/2006 15:04")
}

func permitidas(rol auth.Rol) []string {
	salida := []string{}
	for _, cat := range []string{
		notifications.CatInventario, notifications.CatRecepcion, notifications.CatIoT,
		notifications.CatMantenimiento, notifications.CatSeguridad, notifications.CatAuditoria,
		notifications.CatIA, notifications.CatSistema,
	} {
		if categoriaPermitida(rol, cat) {
			salida = append(salida, cat)
		}
	}
	return salida
}

func categoriaPermitida(rol auth.Rol, cat string) bool {
	for _, r := range notifications.RolesPorCategoria(cat) {
		if r == string(rol) {
			return true
		}
	}
	return rol == auth.RolAdmin
}

func categoriasPorDefecto(permitidas []string) map[string]bool {
	m := make(map[string]bool, len(permitidas))
	for _, c := range permitidas {
		m[c] = true
	}
	return m
}

func contiene(lista []string, valor string) bool {
	for _, v := range lista {
		if v == valor {
			return true
		}
	}
	return false
}

func generarCodigo() string {
	const letras = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		ahora := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(ahora >> (i * 4))
		}
	}
	for i := range b {
		b[i] = letras[int(b[i])%len(letras)]
	}
	return string(b)
}

func enmascararChat(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 4 {
		return id
	}
	return "***" + id[len(id)-4:]
}

func parseChatID(id string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	return n
}
