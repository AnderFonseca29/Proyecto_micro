package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/config"
	"gestock-iot-backend/notifications"
	"gestock-iot-backend/reports"
	"gestock-iot-backend/store"
)

// Mensaje del historial de conversación enviado por Angular.
type Mensaje struct {
	Rol       string `json:"rol"` // usuario | asistente
	Contenido string `json:"contenido"`
}

// Respuesta del agente.
type Respuesta struct {
	Respuesta    string   `json:"respuesta"`
	Herramientas []string `json:"herramientas"`
	Proveedor    string   `json:"proveedor"`
	Modelo       string   `json:"modelo"`
	Modo         string   `json:"modo"` // proveedor | local
	Aviso        string   `json:"aviso,omitempty"`
}

// Estado público (sin secretos) para la interfaz.
type Estado struct {
	Disponible   bool     `json:"disponible"`
	Modo         string   `json:"modo"`
	Proveedor    string   `json:"proveedor"`
	Modelo       string   `json:"modelo"`
	LimiteMinuto int      `json:"limite_por_minuto"`
	Sugerencias  []string `json:"sugerencias"`
}

// Servicio implementa el agente GESTOCK AI. Nunca accede a SQL: solo usa las
// herramientas controladas de tools.go, que validan permisos del usuario.
type Servicio struct {
	cfg          config.Config
	store        *store.Store
	auth         *auth.Servicio
	reportes     *reports.Servicio
	notif        *notifications.Service
	cliente      *http.Client

	mu   sync.Mutex
	usos map[string][]time.Time
}

// Nuevo construye el servicio.
func Nuevo(cfg config.Config, st *store.Store, authSvc *auth.Servicio, rep *reports.Servicio, notif *notifications.Service) *Servicio {
	return &Servicio{
		cfg: cfg, store: st, auth: authSvc, reportes: rep, notif: notif,
		cliente: &http.Client{Timeout: cfg.AITimeout},
		usos:    make(map[string][]time.Time),
	}
}

// Remitente permite generar alertas de IA desde otros módulos sin exponer
// detalles del proveedor.
func (s *Servicio) Remitente() *notifications.Service { return s.notif }

// Estado indica el modo efectivo: proveedor externo o analizador local.
func (s *Servicio) Estado() Estado {
	modo := "local"
	proveedor := "analizador local"
	if s.externoDisponible() {
		modo = "proveedor"
		proveedor = s.cfg.AIProvider
	}
	return Estado{
		Disponible:   s.cfg.AIEnabled || s.cfg.AILocalFallback,
		Modo:         modo,
		Proveedor:    proveedor,
		Modelo:       s.cfg.AIModel,
		LimiteMinuto: s.cfg.AIRateLimit,
		Sugerencias:  Sugerencias,
	}
}

// Sugerencias son preguntas de ejemplo según el uso general.
var Sugerencias = []string{
	"¿Cuántos productos hay y cuál es el estado del inventario?",
	"¿Qué productos tienen stock bajo o están agotados?",
	"¿Cuáles fueron los últimos movimientos?",
	"¿Cómo está la conexión del ESP32 y MQTT?",
	"¿Hay incidencias de mantenimiento abiertas?",
	"Genera un reporte de stock bajo.",
}

func (s *Servicio) externoDisponible() bool {
	return s.cfg.AIEnabled && s.cfg.AIAPIKey != "" && s.cfg.AIBaseURL != ""
}

// Chat procesa una conversación. Si el proveedor externo falla, degrada al
// analizador local para no bloquear al usuario, informando del cambio.
func (s *Servicio) Chat(ctx context.Context, u auth.Usuario, mensajes []Mensaje) (Respuesta, error) {
	if !u.TienePermiso(auth.PermIAUsar) {
		return Respuesta{}, errors.New("no tienes permiso para usar GESTOCK AI")
	}
	if !s.cfg.AIEnabled && !s.cfg.AILocalFallback {
		return Respuesta{}, errors.New("GESTOCK AI está desactivado")
	}
	if err := s.limitar(u.ID); err != nil {
		return Respuesta{}, err
	}
	mensajes = normalizarHistorial(mensajes, s.cfg.AIHistorial)
	if len(mensajes) == 0 {
		return Respuesta{}, errors.New("escribe una pregunta para GESTOCK AI")
	}
	ultimo := mensajes[len(mensajes)-1]
	if len([]rune(ultimo.Contenido)) > 4000 {
		return Respuesta{}, errors.New("la pregunta supera el límite de 4000 caracteres")
	}

	if s.externoDisponible() {
		r, err := s.chatExterno(ctx, u, mensajes)
		if err == nil {
			s.auditar(u, "IA_CONSULTA", "Consulta atendida por proveedor externo", r.Herramientas)
			return r, nil
		}
		log.Printf("[ai] proveedor externo no disponible: %v", err)
		rLocal := s.chatLocal(u, ultimo.Contenido)
		rLocal.Aviso = "El proveedor externo de IA no respondió. Respondí con el analizador local de GESTOCK usando datos reales."
		s.auditar(u, "IA_FALLBACK", "Proveedor externo no disponible; respuesta local", rLocal.Herramientas)
		return rLocal, nil
	}

	r := s.chatLocal(u, ultimo.Contenido)
	s.auditar(u, "IA_CONSULTA", "Consulta atendida por analizador local", r.Herramientas)
	return r, nil
}

// --- proveedor compatible con OpenAI ---

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type mensajeProveedor struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type respuestaProveedor struct {
	Choices []struct {
		Message      mensajeProveedor `json:"message"`
		FinishReason string           `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (s *Servicio) chatExterno(ctx context.Context, u auth.Usuario, historial []Mensaje) (Respuesta, error) {
	msgs := []mensajeProveedor{{Role: "system", Content: promptSistema}}
	for _, m := range historial {
		rol := "user"
		if m.Rol == "asistente" || m.Rol == "assistant" {
			rol = "assistant"
		}
		msgs = append(msgs, mensajeProveedor{Role: rol, Content: m.Contenido})
	}

	usadas := make([]string, 0)
	for ronda := 0; ronda < maxInt(s.cfg.AIMaxRondas, 1); ronda++ {
		cuerpo := map[string]any{
			"model":       s.cfg.AIModel,
			"messages":    msgs,
			"tools":       definicionesOpenAI(),
			"tool_choice": "auto",
			"temperature": s.cfg.AITemperatura,
			"max_tokens":  s.cfg.AIMaxTokens,
		}
		resp, err := s.postProveedor(ctx, cuerpo)
		if err != nil {
			return Respuesta{}, err
		}
		if len(resp.Choices) == 0 {
			return Respuesta{}, errors.New("el proveedor devolvió una respuesta vacía")
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return Respuesta{
				Respuesta:    strings.TrimSpace(msg.Content),
				Herramientas: usadas,
				Proveedor:    s.cfg.AIProvider,
				Modelo:       s.cfg.AIModel,
				Modo:         "proveedor",
			}, nil
		}
		msgs = append(msgs, msg)
		for _, tc := range msg.ToolCalls {
			res := s.ejecutarHerramienta(tc.Function.Name, json.RawMessage(tc.Function.Arguments), u)
			usadas = append(usadas, tc.Function.Name)
			datos, _ := json.Marshal(res)
			msgs = append(msgs, mensajeProveedor{
				Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(datos),
			})
		}
	}
	return Respuesta{}, errors.New("el agente superó el número máximo de rondas de herramientas")
}

func (s *Servicio) postProveedor(ctx context.Context, cuerpo map[string]any) (respuestaProveedor, error) {
	datos, err := json.Marshal(cuerpo)
	if err != nil {
		return respuestaProveedor{}, err
	}
	url := strings.TrimRight(s.cfg.AIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(datos))
	if err != nil {
		return respuestaProveedor{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.AIAPIKey)
	if strings.EqualFold(s.cfg.AIProvider, "openrouter") {
		req.Header.Set("HTTP-Referer", "https://gestock.local")
		req.Header.Set("X-Title", "GESTOCK AI")
	}
	res, err := s.cliente.Do(req)
	if err != nil {
		return respuestaProveedor{}, fmt.Errorf("no se pudo contactar al proveedor: %w", err)
	}
	defer res.Body.Close()
	cuerpoResp, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return respuestaProveedor{}, err
	}
	var r respuestaProveedor
	if err := json.Unmarshal(cuerpoResp, &r); err != nil {
		return respuestaProveedor{}, fmt.Errorf("respuesta inválida del proveedor (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detalle := "error del proveedor"
		if r.Error != nil && r.Error.Message != "" {
			detalle = r.Error.Message
		}
		return respuestaProveedor{}, fmt.Errorf("proveedor HTTP %d: %s", res.StatusCode, limitar(detalle, 240))
	}
	if r.Error != nil {
		return respuestaProveedor{}, errors.New(limitar(r.Error.Message, 240))
	}
	return r, nil
}

// --- analizador local determinista (sin inventar datos) ---

var reCodigo = regexp.MustCompile(`(?i)\bPROD-[0-9]{3,8}\b`)

func (s *Servicio) chatLocal(u auth.Usuario, pregunta string) Respuesta {
	texto := strings.ToLower(strings.TrimSpace(pregunta))
	usadas := make([]string, 0)
	secciones := make([]string, 0)

	llamar := func(nombre string, args map[string]any) (Resultado, bool) {
		raw, _ := json.Marshal(args)
		r := s.ejecutarHerramienta(nombre, raw, u)
		usadas = append(usadas, nombre)
		if !r.OK {
			secciones = append(secciones, "**Resumen**\n"+r.Error)
			return r, false
		}
		return r, true
	}

	// 1. Reportes
	if contieneAlguno(texto, "reporte", "informe", "exportar", "csv", "excel") {
		tipo := "inventario"
		switch {
		case contieneAlguno(texto, "stock bajo", "reposición", "reposicion"):
			tipo = "stock_bajo"
		case contieneAlguno(texto, "movimiento"):
			tipo = "movimientos"
		case contieneAlguno(texto, "recepción", "recepcion", "ingreso"):
			tipo = "recepciones"
		case contieneAlguno(texto, "incidencia", "mantenimiento"):
			tipo = "incidencias"
		case contieneAlguno(texto, "auditoría", "auditoria", "seguridad"):
			tipo = "auditoria"
		}
		if r, ok := llamar("generar_reporte", map[string]any{"tipo": tipo}); ok {
			secciones = append(secciones, fmt.Sprintf("**Resumen**\nGeneré el reporte de %s con los datos disponibles en este momento.", etiquetaReporte(tipo)))
			if d, ok := r.Datos.(map[string]any); ok {
				secciones = append(secciones, fmt.Sprintf("**Datos encontrados**\n- Archivo: %v\n- Descarga: `%v`\n- Generado: %v", d["nombre"], d["descarga"], d["generado_en"]))
			}
			secciones = append(secciones, "**Próximo paso**\nDescarga el archivo desde el botón del reporte o desde la sección Reportes. El archivo incluye la fecha de generación y la fuente de los datos.")
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 2. Producto concreto
	if cod := reCodigo.FindString(pregunta); cod != "" {
		if r, ok := llamar("consultar_producto", map[string]any{"codigo": cod}); ok {
			secciones = append(secciones, formatearProducto(r))
			secciones = append(secciones, "**Próximo paso**\nSi el stock está bajo, programa una reposición desde el módulo de inventario.")
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 3. Stock bajo / agotados / reposición
	if contieneAlguno(texto, "stock bajo", "agotad", "reposición", "reposicion", "reponer", "mínimo", "minimo", "faltante", "abastecimiento") {
		if r, ok := llamar("consultar_stock_bajo", map[string]any{}); ok {
			secciones = append(secciones, formatearStockBajo(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 4. IoT / ESP32 / MQTT
	if contieneAlguno(texto, "esp32", "mqtt", "sensor", "servo", "iot", "clasificación física", "clasificacion fisica", "desconect") &&
		!contieneAlguno(texto, "recepción", "recepcion", "ingreso") {
		if r, ok := llamar("consultar_alertas_iot", map[string]any{}); ok {
			secciones = append(secciones, formatearIoT(r))
			secciones = append(secciones, "**Próximo paso**\nUn técnico debe verificar alimentación, Wi-Fi y credenciales MQTT. Una pérdida de comunicación no confirma por sí sola una avería del hardware.")
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 5. Mantenimiento
	if contieneAlguno(texto, "incidencia", "mantenimiento", "técnico", "tecnico", "revisar", "avería", "averia") {
		if r, ok := llamar("consultar_mantenimiento", map[string]any{}); ok {
			secciones = append(secciones, formatearIncidencias(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 6. Usuarios
	if contieneAlguno(texto, "usuario", "usuarios", "roles", "permisos") && !contieneAlguno(texto, "actividad", "auditor") {
		if r, ok := llamar("consultar_usuarios", map[string]any{}); ok {
			secciones = append(secciones, formatearUsuarios(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 7. Auditoría
	if contieneAlguno(texto, "auditoría", "auditoria", "seguridad", "actividad", "historial de operaciones") {
		if r, ok := llamar("consultar_auditoria", map[string]any{}); ok {
			secciones = append(secciones, formatearAuditoria(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 8. Recepciones
	if contieneAlguno(texto, "recepción", "recepcion", "recepciones", "recib", "ingreso", "pendiente de clasificación", "pendiente de clasificacion") {
		estado := ""
		if contieneAlguno(texto, "fall", "error", "desconocid") {
			estado = "DESCONOCIDO"
		}
		if r, ok := llamar("consultar_recepciones", map[string]any{"estado": estado, "limite": 10}); ok {
			secciones = append(secciones, formatearRecepciones(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 9. Movimientos
	if contieneAlguno(texto, "movimiento", "entrada", "salida", "ajuste", "rotación", "rotacion", "reciente") {
		tipo := ""
		if contieneAlguno(texto, "entrada", "ingreso") {
			tipo = "ENTRADA"
		} else if contieneAlguno(texto, "salida") {
			tipo = "SALIDA"
		} else if contieneAlguno(texto, "ajuste") {
			tipo = "AJUSTE"
		}
		if r, ok := llamar("consultar_movimientos", map[string]any{"tipo": tipo, "limite": 15}); ok {
			secciones = append(secciones, formatearMovimientos(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 10. Inventario general
	if contieneAlguno(texto, "producto", "inventario", "existencia", "cuánto", "cuanto", "disponible", "resumen", "estado") {
		if r, ok := llamar("consultar_inventario", map[string]any{"limite": 100}); ok {
			secciones = append(secciones, formatearInventario(r))
			return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
		}
	}

	// 11. Resumen ejecutivo por defecto (solo con permisos).
	if u.TienePermiso(auth.PermInventarioLeer) {
		if r, ok := llamar("consultar_inventario", map[string]any{"limite": 100}); ok {
			secciones = append(secciones, formatearInventario(r))
		}
	}
	if u.TienePermiso(auth.PermIoTLeer) {
		if r, ok := llamar("consultar_alertas_iot", map[string]any{}); ok {
			secciones = append(secciones, formatearIoT(r))
		}
	}
	if u.TienePermiso(auth.PermMovimientosLeer) {
		if r, ok := llamar("consultar_movimientos", map[string]any{"limite": 5}); ok {
			secciones = append(secciones, formatearMovimientos(r))
		}
	}
	if len(secciones) == 0 {
		secciones = append(secciones, "**Resumen**\nNo tengo información suficiente para responder con los permisos de tu rol. Puedes preguntar por inventario, recepciones, incidencias o IoT según tu acceso.")
	} else {
		secciones = append([]string{"**Resumen**\nNo reconocí una pregunta concreta; este es el estado general de GESTOCK que puedes consultar con tu rol."}, secciones...)
	}
	return Respuesta{Respuesta: strings.Join(secciones, "\n\n"), Herramientas: usadas, Proveedor: "analizador local", Modelo: "reglas-gestock", Modo: "local"}
}

// --- formateadores ---

func formatearInventario(r Resultado) string {
	d, _ := r.Datos.(map[string]any)
	productos, _ := d["productos"].([]store.Producto)
	resumen, _ := d["resumen"].(store.ResumenInventario)
	lineas := []string{
		"**Resumen**",
		fmt.Sprintf("Hay %d productos registrados, %d activos y %d unidades disponibles.", resumen.TotalProductos, resumen.ProductosActivos, resumen.UnidadesDisponibles),
		"",
		"**Datos encontrados**",
	}
	if len(productos) == 0 {
		lineas = append(lineas, "No se encontraron productos con ese criterio.")
	} else {
		for i, p := range productos {
			if i >= 12 {
				lineas = append(lineas, fmt.Sprintf("… y %d productos más.", len(productos)-12))
				break
			}
			estado := "OK"
			if p.Stock == 0 {
				estado = "AGOTADO"
			} else if p.Stock <= p.StockMinimo {
				estado = "STOCK BAJO"
			}
			lineas = append(lineas, fmt.Sprintf("• %s — %s: %d uds (mín. %d) · %s", p.Codigo, p.Nombre, p.Stock, p.StockMinimo, estado))
		}
	}
	if resumen.ProductosStockBajo > 0 {
		lineas = append(lineas, "",
			"**Análisis**",
			fmt.Sprintf("%d producto(s) están en o por debajo del mínimo y %d están agotados. Son candidatos a reposición.", resumen.ProductosStockBajo, resumen.ProductosAgotados),
			"",
			"**Recomendación (no confirmada)**",
			"Revisa la sección de stock bajo y confirma las cantidades antes de generar una orden de compra.")
	}
	lineas = append(lineas, "", "**Próximo paso**", "Puedes pedirme «stock bajo», «últimos movimientos» o «genera un reporte de inventario».")
	return strings.Join(lineas, "\n")
}

func formatearStockBajo(r Resultado) string {
	d, _ := r.Datos.(map[string]any)
	productos, _ := d["productos"].([]store.Producto)
	lineas := []string{"**Resumen**"}
	if len(productos) == 0 {
		lineas = append(lineas, "No hay productos por debajo del stock mínimo en este momento.")
		lineas = append(lineas, "", "**Datos encontrados**", "La consulta se realizó sobre el catálogo actual del backend y devolvió 0 coincidencias.")
		return strings.Join(lineas, "\n")
	}
	agotados := 0
	for _, p := range productos {
		if p.Stock == 0 {
			agotados++
		}
	}
	lineas = append(lineas, fmt.Sprintf("Hay %d productos que requieren atención: %d agotados y %d con stock bajo.", len(productos), agotados, len(productos)-agotados))
	lineas = append(lineas, "", "**Datos encontrados**")
	for _, p := range productos {
		faltan := p.StockMinimo - p.Stock
		if faltan < 0 {
			faltan = 0
		}
		lineas = append(lineas, fmt.Sprintf("• %s — %s: %d/%d uds (faltan %d para el mínimo)", p.Codigo, p.Nombre, p.Stock, p.StockMinimo, faltan))
	}
	lineas = append(lineas, "", "**Análisis**", "Los productos agotados afectan la operación inmediata; los que están bajo el mínimo deben reponerse antes del próximo ciclo de consumo.")
	lineas = append(lineas, "", "**Recomendación (no confirmada)**", "Prioriza los agotados, verifica las existencias físicas y genera una solicitud de reposición. La recomendación se basa en el stock registrado, no en una compra confirmada.")
	lineas = append(lineas, "", "**Próximo paso**", "Genera el reporte «stock bajo» para compartir la lista con el responsable de compras.")
	return strings.Join(lineas, "\n")
}

func formatearMovimientos(r Resultado) string {
	movs, _ := r.Datos.([]store.Movimiento)
	lineas := []string{"**Resumen**", fmt.Sprintf("Encontré %d movimientos recientes.", len(movs)), "", "**Datos encontrados**"}
	if len(movs) == 0 {
		lineas = append(lineas, "No hay movimientos registrados para el filtro solicitado.")
	} else {
		for i, m := range movs {
			if i >= 15 {
				lineas = append(lineas, "…")
				break
			}
			signo := ""
			if m.Cantidad > 0 && m.Tipo != store.MovAjuste {
				signo = "+"
			}
			lineas = append(lineas, fmt.Sprintf("• %s %s %s %s%d → stock %d · %s · %s", fechaCorta(m.FechaRegistro), m.Tipo, m.CodigoProducto, signo, m.Cantidad, m.StockNuevo, m.Usuario, m.Motivo))
		}
	}
	lineas = append(lineas, "", "**Próximo paso**", "Puedes filtrar por ENTRADA, SALIDA o AJUSTE, o solicitar un reporte de movimientos.")
	return strings.Join(lineas, "\n")
}

func formatearRecepciones(r Resultado) string {
	d, _ := r.Datos.(map[string]any)
	recs, _ := d["recepciones"].([]store.Recepcion)
	pendientes, _ := d["pendientes"].([]store.Recepcion)
	lineas := []string{"**Resumen**", fmt.Sprintf("Encontré %d recepciones y %d pendientes de confirmación física.", len(recs), len(pendientes)), "", "**Datos encontrados**"}
	if len(recs) == 0 {
		lineas = append(lineas, "No hay recepciones registradas para el filtro solicitado.")
	} else {
		for i, rec := range recs {
			if i >= 12 {
				lineas = append(lineas, "…")
				break
			}
			lineas = append(lineas, fmt.Sprintf("• %s — %s (%s) · %d uds · registro %s · clasificación %s", fechaCorta(rec.FechaRegistro), rec.CodigoProducto, rec.NombreProducto, rec.Cantidad, rec.Estado, valorODefecto(rec.Clasificacion, "N/A")))
		}
	}
	lineas = append(lineas, "", "**Análisis**", "El registro en inventario y la clasificación física son estados distintos. Una recepción puede estar registrada aunque el ESP32 aún no haya confirmado el movimiento de la caja.")
	lineas = append(lineas, "", "**Próximo paso**", "Si hay clasificaciones pendientes, revisa la página de Recepción y reintenta la orden MQTT.")
	return strings.Join(lineas, "\n")
}

func formatearIoT(r Resultado) string {
	d, _ := r.Datos.(map[string]any)
	e, _ := d["estado"].(store.EstadoIoT)
	fallidas, _ := d["clasificaciones_fallidas"].([]store.Recepcion)
	lineas := []string{
		"**Resumen**",
		fmt.Sprintf("ESP32: %s · MQTT: %s · Sensor: %s · Servo: %s.", siNo(e.ESP32Online, "en línea", "desconectado"), siNo(e.MQTTConectado, "conectado", "desconectado"), siNo(e.Sensor.CajaPresente, "caja detectada", "sin caja"), valorODefecto(e.Servo, "sin dato")),
		"",
		"**Datos encontrados**",
		fmt.Sprintf("• Última comunicación ESP32: %s", fechaHumana(e.ESP32LastSeen)),
		fmt.Sprintf("• Detecciones del sensor: %d", e.Sensor.Detecciones),
		fmt.Sprintf("• Clasificación actual: %s", valorODefecto(e.Clasificacion, "sin dato")),
		fmt.Sprintf("• Último mensaje: %s", valorODefecto(e.UltimoMensaje, "sin dato")),
		fmt.Sprintf("• Recepciones registradas: %d · desconocidas: %d · unidades hoy: %d", e.Registrados, e.Desconocidos, e.UnidadesHoy),
	}
	if len(fallidas) > 0 {
		lineas = append(lineas, "", "**Clasificaciones fallidas recientes**")
		for i, f := range fallidas {
			if i >= 8 {
				break
			}
			lineas = append(lineas, fmt.Sprintf("• %s — %s · %s", fechaCorta(f.FechaRegistro), f.CodigoProducto, f.NombreProducto))
		}
	}
	lineas = append(lineas, "", "**Análisis**", "Los datos anteriores describen el estado reportado por el sistema. No confirman una avería física: una desconexión puede deberse a alimentación, Wi-Fi, credenciales MQTT o al broker.")
	lineas = append(lineas, "", "**Recomendación (no confirmada)**", "Verifica en orden: alimentación del ESP32, red Wi-Fi, conexión al broker y funcionamiento del servo/sensor. Revisa las incidencias de mantenimiento abiertas.")
	lineas = append(lineas, "", "**Próximo paso**", "Si el problema persiste, registra una incidencia de mantenimiento con el dispositivo y la hora del último evento.")
	return strings.Join(lineas, "\n")
}

func formatearIncidencias(r Resultado) string {
	incs, _ := r.Datos.([]store.Incidencia)
	abiertas := 0
	for _, in := range incs {
		if in.Estado != "RESUELTA" {
			abiertas++
		}
	}
	lineas := []string{"**Resumen**", fmt.Sprintf("Hay %d incidencias registradas y %d sin resolver.", len(incs), abiertas), "", "**Datos encontrados**"}
	if len(incs) == 0 {
		lineas = append(lineas, "No hay incidencias de mantenimiento registradas.")
	} else {
		for i, in := range incs {
			if i >= 12 {
				break
			}
			lineas = append(lineas, fmt.Sprintf("• #%d [%s] %s · %s · dispositivo %s · creada por %s", in.ID, in.Prioridad, in.Titulo, in.Estado, valorODefecto(in.Dispositivo, "N/A"), in.CreadaPor))
		}
	}
	lineas = append(lineas, "", "**Próximo paso**", "El técnico puede actualizar el estado desde el módulo de Mantenimiento. Las incidencias resueltas conservan su resolución.")
	return strings.Join(lineas, "\n")
}

func formatearUsuarios(r Resultado) string {
	usuarios, _ := r.Datos.([]auth.Publico)
	lineas := []string{"**Resumen**", fmt.Sprintf("Hay %d usuarios registrados.", len(usuarios)), "", "**Datos encontrados**"}
	for i, u := range usuarios {
		if i >= 20 {
			break
		}
		lineas = append(lineas, fmt.Sprintf("• %s (%s) · rol %s · %s", u.Nombre, u.Usuario, u.Rol, siNo(u.Activo, "activo", "inactivo")))
	}
	lineas = append(lineas, "", "**Próximo paso**", "Los permisos se validan siempre en el backend. No compartas credenciales por este chat.")
	return strings.Join(lineas, "\n")
}

func formatearAuditoria(r Resultado) string {
	eventos, _ := r.Datos.([]store.Auditoria)
	lineas := []string{"**Resumen**", fmt.Sprintf("Encontré %d eventos de auditoría recientes.", len(eventos)), "", "**Datos encontrados**"}
	if len(eventos) == 0 {
		lineas = append(lineas, "No hay eventos de auditoría registrados.")
	} else {
		for i, a := range eventos {
			if i >= 15 {
				break
			}
			lineas = append(lineas, fmt.Sprintf("• %s [%s] %s · %s · usuario %s (%s)", fechaCorta(a.Fecha), a.Severidad, a.Evento, a.Detalle, a.Usuario, a.Rol))
		}
	}
	lineas = append(lineas, "", "**Próximo paso**", "Los eventos mostrados dependen de tu rol. Para una revisión formal, genera un reporte de auditoría.")
	return strings.Join(lineas, "\n")
}

func formatearProducto(r Resultado) string {
	switch d := r.Datos.(type) {
	case store.Producto:
		estado := "OK"
		if d.Stock == 0 {
			estado = "AGOTADO"
		} else if d.Stock <= d.StockMinimo {
			estado = "STOCK BAJO"
		}
		return fmt.Sprintf("**Resumen**\nProducto encontrado: %s (%s).\n\n**Datos encontrados**\n- Categoría: %s\n- Stock actual: %d uds\n- Stock mínimo: %d uds\n- Stock inicial: %d uds\n- Estado: %s\n\n**Próximo paso**\nPuedes consultar sus movimientos o generar un reporte de inventario.", d.Nombre, d.Codigo, valorODefecto(d.Categoria, "sin categoría"), d.Stock, d.StockMinimo, d.StockInicial, estado)
	case map[string]any:
		if productos, ok := d["productos"].([]store.Producto); ok {
			return formatearInventario(Resultado{Datos: map[string]any{"productos": productos, "resumen": store.ResumenInventario{}}})
		}
	}
	return "**Resumen**\nNo encontré el producto solicitado en el catálogo actual."
}

// --- utilidades ---

func normalizarHistorial(mensajes []Mensaje, maximo int) []Mensaje {
	if maximo <= 0 {
		maximo = 10
	}
	salida := make([]Mensaje, 0, len(mensajes))
	for _, m := range mensajes {
		rol := strings.ToLower(strings.TrimSpace(m.Rol))
		if rol != "usuario" && rol != "user" && rol != "asistente" && rol != "assistant" {
			continue
		}
		if rol == "user" {
			rol = "usuario"
		}
		if rol == "assistant" {
			rol = "asistente"
		}
		contenido := strings.TrimSpace(m.Contenido)
		if contenido == "" {
			continue
		}
		salida = append(salida, Mensaje{Rol: rol, Contenido: limitar(contenido, 4000)})
	}
	if len(salida) > maximo*2 {
		salida = salida[len(salida)-maximo*2:]
	}
	return salida
}

func (s *Servicio) limitar(usuarioID string) error {
	limite := s.cfg.AIRateLimit
	if limite <= 0 {
		limite = 20
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ahora := time.Now()
	hist := s.usos[usuarioID]
	vigentes := hist[:0]
	for _, t := range hist {
		if ahora.Sub(t) < time.Minute {
			vigentes = append(vigentes, t)
		}
	}
	if len(vigentes) >= limite {
		s.usos[usuarioID] = vigentes
		return fmt.Errorf("límite de consultas de IA alcanzado (%d por minuto); espera un momento", limite)
	}
	s.usos[usuarioID] = append(vigentes, ahora)
	return nil
}

func (s *Servicio) auditar(u auth.Usuario, evento, detalle string, herramientas []string) {
	if s.store == nil {
		return
	}
	extra := ""
	if len(herramientas) > 0 {
		ordenadas := append([]string(nil), herramientas...)
		sort.Strings(ordenadas)
		extra = " · herramientas: " + strings.Join(unicos(ordenadas), ", ")
	}
	s.store.RegistrarAuditoria(store.Auditoria{
		Evento: evento, Detalle: limitar(detalle+extra, 500),
		Usuario: u.Nombre, Rol: string(u.Rol), Severidad: "INFO",
	})
}

func unicos(lista []string) []string {
	vistos := map[string]bool{}
	salida := make([]string, 0, len(lista))
	for _, v := range lista {
		if !vistos[v] {
			vistos[v] = true
			salida = append(salida, v)
		}
	}
	return salida
}

func contieneAlguno(texto string, claves ...string) bool {
	for _, k := range claves {
		if strings.Contains(texto, k) {
			return true
		}
	}
	return false
}

func valorODefecto(v, defecto string) string {
	if strings.TrimSpace(v) == "" || v == "—" {
		return defecto
	}
	return v
}

func siNo(v bool, si, no string) string {
	if v {
		return si
	}
	return no
}

func etiquetaReporte(tipo string) string {
	switch tipo {
	case "stock_bajo":
		return "stock bajo"
	case "movimientos":
		return "movimientos"
	case "recepciones":
		return "recepciones"
	case "incidencias":
		return "incidencias"
	case "auditoria":
		return "auditoría"
	default:
		return "inventario"
	}
}

func fechaCorta(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return valorODefecto(iso, "sin fecha")
	}
	return t.Local().Format("02/01 15:04")
}

func fechaHumana(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return "sin registro"
	}
	return t.Local().Format("02/01/2006 15:04")
}

func limitar(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
