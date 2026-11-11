// Package realtime expone un hub WebSocket autenticado y efímero para
// notificaciones, estado IoT y eventos de recepción en tiempo real.
package realtime

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"gestock-iot-backend/auth"
)

// Evento es el sobre que consume Angular.
type Evento struct {
	Tipo  string `json:"type"`
	Data  any    `json:"data"`
	Fecha string `json:"fecha"`
}

type ticket struct {
	usuario auth.Usuario
	expira  time.Time
}

type cliente struct {
	hub     *Hub
	conn    *websocket.Conn
	usuario auth.Usuario
	enviar  chan []byte
}

// Hub mantiene las conexiones activas y los tickets de un solo uso.
type Hub struct {
	mu         sync.RWMutex
	clientes   map[*cliente]struct{}
	tickets    map[string]ticket
	origenes   []string
	upgrader   websocket.Upgrader
}

// Nuevo crea el hub. origenes son los CORS permitidos.
func Nuevo(origenes []string) *Hub {
	h := &Hub{
		clientes: make(map[*cliente]struct{}),
		tickets:  make(map[string]ticket),
		origenes: origenes,
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origen := r.Header.Get("Origin")
			if origen == "" {
				return true // clientes nativos / pruebas
			}
			u, err := url.Parse(origen)
			if err != nil {
				return false
			}
			for _, permitido := range origenes {
				if permitido == "*" {
					return true
				}
				p, err := url.Parse(strings.TrimRight(permitido, "/"))
				if err == nil && strings.EqualFold(p.Host, u.Host) {
					return true
				}
			}
			return false
		},
	}
	go h.limpiarTickets()
	return h
}

// CrearTicket genera un ticket de un solo uso (60 s) para abrir el WebSocket.
// El JWT nunca viaja en la URL del WebSocket.
func (h *Hub) CrearTicket(u auth.Usuario) string {
	t := generarID()
	h.mu.Lock()
	// Limpieza oportunista.
	for k, v := range h.tickets {
		if time.Now().After(v.expira) {
			delete(h.tickets, k)
		}
	}
	h.tickets[t] = ticket{usuario: u, expira: time.Now().Add(60 * time.Second)}
	h.mu.Unlock()
	return t
}

// ServeWS valida el ticket, actualiza la conexión y arranca el bombeo.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, ticketID string) {
	h.mu.Lock()
	t, ok := h.tickets[ticketID]
	if ok {
		delete(h.tickets, ticketID) // un solo uso
	}
	h.mu.Unlock()
	if !ok || time.Now().After(t.expira) {
		http.Error(w, "ticket inválido o expirado", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &cliente{hub: h, conn: conn, usuario: t.usuario, enviar: make(chan []byte, 64)}
	h.mu.Lock()
	h.clientes[c] = struct{}{}
	h.mu.Unlock()

	go c.escribir()
	go c.leer()
}

// Emitir difunde un evento a los usuarios que cumplan el filtro (nil = todos).
func (h *Hub) Emitir(tipo string, data any, filtro func(auth.Usuario) bool) {
	msg, err := json.Marshal(Evento{Tipo: tipo, Data: data, Fecha: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clientes {
		if filtro != nil && !filtro(c.usuario) {
			continue
		}
		select {
		case c.enviar <- msg:
		default:
			// Cliente lento: se descarta el evento (no se bloquea el backend).
		}
	}
}

// Conexiones devuelve el número de clientes conectados.
func (h *Hub) Conexiones() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clientes)
}

func (h *Hub) quitar(c *cliente) {
	h.mu.Lock()
	if _, ok := h.clientes[c]; ok {
		delete(h.clientes, c)
		close(c.enviar)
	}
	h.mu.Unlock()
}

func (c *cliente) leer() {
	defer func() {
		c.hub.quitar(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *cliente) escribir() {
	ticker := time.NewTicker(25 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.enviar:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) limpiarTickets() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for range t.C {
		h.mu.Lock()
		for k, v := range h.tickets {
			if time.Now().After(v.expira) {
				delete(h.tickets, k)
			}
		}
		h.mu.Unlock()
	}
}

func generarID() string {
	const letras = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	ahora := time.Now().UnixNano()
	for i := range b {
		ahora = ahora*6364136223846793005 + 1442695040888963407
		b[i] = letras[int((ahora>>33)&0x7fffffff)%len(letras)]
	}
	return string(b)
}

// LogConexion deja traza sin datos sensibles.
func LogConexion(usuario string) {
	log.Printf("[ws] usuario conectado: %s", usuario)
}
