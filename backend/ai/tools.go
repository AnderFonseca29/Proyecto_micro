package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gestock-iot-backend/auth"
	"gestock-iot-backend/reports"
	"gestock-iot-backend/store"
)

// Resultado de una herramienta. Se devuelve al modelo y también a Angular.
type Resultado struct {
	OK      bool   `json:"ok"`
	Resumen string `json:"resumen"`
	Datos   any    `json:"datos,omitempty"`
	Error   string `json:"error,omitempty"`
}

type herramienta struct {
	Nombre      string
	Descripcion string
	Parametros  map[string]any
	Permiso     auth.Permiso
}

// definiciones describe las herramientas para el proveedor (function calling).
func definiciones() []herramienta {
	return []herramienta{
		{
			Nombre: "consultar_inventario", Permiso: auth.PermInventarioLeer,
			Descripcion: "Consulta el inventario actual. Permite filtrar por texto, categoría y límite.",
			Parametros: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"q":         map[string]any{"type": "string", "description": "Texto a buscar en código, nombre o categoría"},
					"categoria": map[string]any{"type": "string", "description": "Categoría exacta o parcial"},
					"limite":    map[string]any{"type": "integer", "description": "Máximo de productos (1-100)"},
				},
			},
		},
		{
			Nombre: "consultar_producto", Permiso: auth.PermInventarioLeer,
			Descripcion: "Busca un producto concreto por código o nombre.",
			Parametros: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"codigo": map[string]any{"type": "string", "description": "Código o nombre del producto"},
				},
				"required": []string{"codigo"},
			},
		},
		{
			Nombre: "consultar_stock_bajo", Permiso: auth.PermInventarioLeer,
			Descripcion: "Lista productos activos en o por debajo del stock mínimo, incluidos los agotados.",
			Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Nombre: "consultar_movimientos", Permiso: auth.PermMovimientosLeer,
			Descripcion: "Consulta entradas, salidas y ajustes de inventario.",
			Parametros: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tipo":   map[string]any{"type": "string", "enum": []string{"ENTRADA", "SALIDA", "AJUSTE"}},
					"codigo": map[string]any{"type": "string"},
					"limite": map[string]any{"type": "integer"},
				},
			},
		},
		{
			Nombre: "consultar_recepciones", Permiso: auth.PermRecepcionLeer,
			Descripcion: "Consulta recepciones registradas, fallidas o pendientes de clasificación.",
			Parametros: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"estado": map[string]any{"type": "string", "enum": []string{"REGISTRADO", "DESCONOCIDO", "ERROR"}},
					"q":      map[string]any{"type": "string"},
					"limite": map[string]any{"type": "integer"},
				},
			},
		},
		{
			Nombre: "consultar_usuarios", Permiso: auth.PermUsuariosLeer,
			Descripcion: "Lista los usuarios de GESTOCK y sus roles. Solo para quien tenga permiso de usuarios.",
			Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Nombre: "consultar_alertas_iot", Permiso: auth.PermIoTLeer,
			Descripcion: "Consulta el estado del ESP32, MQTT, sensor HC-SR04, servo y clasificaciones fallidas.",
			Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Nombre: "consultar_mantenimiento", Permiso: auth.PermMantenimientoLeer,
			Descripcion: "Consulta incidencias de mantenimiento e IoT registradas.",
			Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Nombre: "consultar_auditoria", Permiso: auth.PermAuditoriaLeer,
			Descripcion: "Consulta registros de auditoría y actividad de usuarios. Solo auditores y administradores.",
			Parametros:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Nombre: "generar_reporte", Permiso: auth.PermReportesGenerar,
			Descripcion: "Genera un reporte CSV verificable y devuelve su enlace de descarga.",
			Parametros: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tipo": map[string]any{
						"type": "string",
						"enum": []string{"inventario", "stock_bajo", "movimientos", "recepciones", "incidencias", "auditoria"},
					},
				},
				"required": []string{"tipo"},
			},
		},
	}
}

// definicionesOpenAI transforma las herramientas al formato del proveedor.
func definicionesOpenAI() []map[string]any {
	salida := make([]map[string]any, 0, len(definiciones()))
	for _, h := range definiciones() {
		params := h.Parametros
		if params["properties"] == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		salida = append(salida, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": h.Nombre, "description": h.Descripcion, "parameters": params,
			},
		})
	}
	return salida
}

func permisoDe(nombre string) (auth.Permiso, bool) {
	for _, h := range definiciones() {
		if h.Nombre == nombre {
			return h.Permiso, true
		}
	}
	return "", false
}

// ejecutarHerramienta valida permisos y consulta datos reales.
func (s *Servicio) ejecutarHerramienta(nombre string, args json.RawMessage, u auth.Usuario) Resultado {
	permiso, ok := permisoDe(nombre)
	if !ok {
		return Resultado{OK: false, Error: "herramienta desconocida"}
	}
	if !u.TienePermiso(permiso) {
		return Resultado{OK: false, Error: "no autorizado para consultar esta información con tu rol"}
	}
	var p struct {
		Q         string `json:"q"`
		Categoria string `json:"categoria"`
		Codigo    string `json:"codigo"`
		Tipo      string `json:"tipo"`
		Estado    string `json:"estado"`
		Limite    int    `json:"limite"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &p)
	}

	switch nombre {
	case "consultar_inventario":
		productos := s.store.BuscarProductos(p.Q)
		if p.Categoria != "" {
			filtrados := productos[:0]
			for _, prod := range productos {
				if strings.Contains(strings.ToLower(prod.Categoria), strings.ToLower(p.Categoria)) {
					filtrados = append(filtrados, prod)
				}
			}
			productos = filtrados
		}
		limite := normalizarLimite(p.Limite, 100)
		if len(productos) > limite {
			productos = productos[:limite]
		}
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d productos encontrados", len(productos)), Datos: map[string]any{
			"productos": productos,
			"resumen":   s.store.Resumen(),
			"nota":      "Datos confirmados por el backend de GESTOCK.",
		}}

	case "consultar_producto":
		if strings.TrimSpace(p.Codigo) == "" {
			return Resultado{OK: false, Error: "debes indicar el código o nombre del producto"}
		}
		if prod, encontrado := s.store.ProductoPorCodigo(p.Codigo); encontrado {
			return Resultado{OK: true, Resumen: "Producto encontrado por código", Datos: prod}
		}
		coincidencias := s.store.BuscarProductos(p.Codigo)
		if len(coincidencias) == 0 {
			return Resultado{OK: true, Resumen: "No existe ningún producto con ese código o nombre.", Datos: map[string]any{"productos": []store.Producto{}}}
		}
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d coincidencias por nombre", len(coincidencias)), Datos: map[string]any{"productos": coincidencias}}

	case "consultar_stock_bajo":
		productos := s.store.ProductosStockBajo()
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d productos en o bajo el mínimo", len(productos)), Datos: map[string]any{
			"productos": productos,
			"resumen":   s.store.Resumen(),
		}}

	case "consultar_movimientos":
		limite := normalizarLimite(p.Limite, 20)
		movs := s.store.Movimientos(p.Tipo, p.Codigo, limite)
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d movimientos encontrados", len(movs)), Datos: movs}

	case "consultar_recepciones":
		limite := normalizarLimite(p.Limite, 20)
		recs := s.store.RecepcionesFiltradas(strings.ToUpper(p.Estado), p.Q, limite)
		pendientes := s.store.Pendientes()
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d recepciones encontradas; %d pendientes de clasificación", len(recs), len(pendientes)), Datos: map[string]any{
			"recepciones": recs, "pendientes": pendientes,
		}}

	case "consultar_usuarios":
		return Resultado{OK: true, Resumen: "Usuarios autorizados", Datos: s.auth.Listar()}

	case "consultar_alertas_iot":
		estado := s.store.EstadoIoT()
		fallidas := []store.Recepcion{}
		for _, r := range s.store.Recepciones() {
			if r.Clasificacion == store.ClasificacionFallida {
				fallidas = append(fallidas, r)
				if len(fallidas) >= 10 {
					break
				}
			}
		}
		return Resultado{OK: true, Resumen: "Estado IoT actual", Datos: map[string]any{
			"estado": estado, "clasificaciones_fallidas": fallidas,
			"nota": "Una pérdida de comunicación no demuestra por sí sola una avería física.",
		}}

	case "consultar_mantenimiento":
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d incidencias registradas", len(s.store.Incidencias())), Datos: s.store.Incidencias()}

	case "consultar_auditoria":
		eventos := s.store.Auditoria(50)
		return Resultado{OK: true, Resumen: fmt.Sprintf("%d eventos de auditoría recientes", len(eventos)), Datos: eventos}

	case "generar_reporte":
		tipo := strings.ToLower(strings.TrimSpace(p.Tipo))
		if tipo == "" {
			tipo = "inventario"
		}
		nombre, contenido, err := reports.Generar(tipo, s.store)
		if err != nil {
			return Resultado{OK: false, Error: err.Error()}
		}
		r := s.reportes.Crear(nombre, contenido, u.ID)
		return Resultado{OK: true, Resumen: "Reporte generado: " + nombre, Datos: map[string]any{
			"reporte_id": r.ID, "nombre": r.Nombre, "descarga": "/api/reportes/" + r.ID,
			"generado_en": r.Creado.Format("2006-01-02 15:04:05 UTC"),
			"nota":        "Archivo CSV con los datos disponibles en el momento de la generación.",
		}}
	}
	return Resultado{OK: false, Error: "herramienta no implementada"}
}

func normalizarLimite(n, porDefecto int) int {
	if n <= 0 {
		return porDefecto
	}
	if n > 100 {
		return 100
	}
	return n
}

// ordenarProductosPorStock ordena ascendente (para recomendaciones).
func ordenarProductosPorStock(p []store.Producto) {
	sort.SliceStable(p, func(i, j int) bool { return p[i].Stock < p[j].Stock })
}
