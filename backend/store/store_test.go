package store

import "testing"

func TestCatalogoInicial(t *testing.T) {
	s := Nuevo()
	productos := s.Productos()
	if len(productos) != 5 {
		t.Fatalf("se esperaban 5 productos, hay %d", len(productos))
	}
	for _, p := range productos {
		if p.Stock != p.StockInicial {
			t.Errorf("%s: stock %d != inicial %d", p.Codigo, p.Stock, p.StockInicial)
		}
		if !p.Activo {
			t.Errorf("%s debería estar activo", p.Codigo)
		}
	}
}

func TestRecepcionSumaStock(t *testing.T) {
	s := Nuevo()
	rec, duplicado, err := s.Recepcionar(SolicitudRecepcion{
		CodigoProducto: "PROD-1234",
		Cantidad:       1,
		IDOperacion:    "op-1",
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if duplicado {
		t.Fatal("no debería ser duplicado")
	}
	if rec.Estado != EstadoRegistrado {
		t.Fatalf("estado %s != REGISTRADO", rec.Estado)
	}
	p, _ := s.ProductoPorCodigo("PROD-1234")
	if p.Stock != 11 {
		t.Fatalf("stock %d != 11", p.Stock)
	}
}

func TestProductoDesconocidoNoModificaStock(t *testing.T) {
	s := Nuevo()
	_, _, err := s.Recepcionar(SolicitudRecepcion{CodigoProducto: "NO-EXISTE"})
	if err != ErrProductoDesconocido {
		t.Fatalf("se esperaba ErrProductoDesconocido, se obtuvo %v", err)
	}
	if len(s.Recepciones()) != 0 {
		t.Fatal("no debería registrarse ningún ingreso")
	}
}

func TestIdempotenciaPorOperacion(t *testing.T) {
	s := Nuevo()
	_, _, _ = s.Recepcionar(SolicitudRecepcion{CodigoProducto: "PROD-1235", IDOperacion: "dup"})
	_, duplicado, err := s.Recepcionar(SolicitudRecepcion{CodigoProducto: "PROD-1235", IDOperacion: "dup"})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if !duplicado {
		t.Fatal("se esperaba operación duplicada")
	}
	p, _ := s.ProductoPorCodigo("PROD-1235")
	if p.Stock != 16 {
		t.Fatalf("el stock no debe duplicarse: %d != 16", p.Stock)
	}
	if len(s.Recepciones()) != 1 {
		t.Fatalf("se esperaba 1 recepción, hay %d", len(s.Recepciones()))
	}
}

func TestClasificacion(t *testing.T) {
	s := Nuevo()
	rec, _, _ := s.Recepcionar(SolicitudRecepcion{CodigoProducto: "PROD-1236", IDOperacion: "op-c"})
	if rec.Clasificacion != ClasificacionPendiente {
		t.Fatalf("clasificación inicial %s != PENDIENTE", rec.Clasificacion)
	}
	if !s.MarcarClasificacion("op-c", true, "ok") {
		t.Fatal("no se encontró la operación")
	}
	guardada, _ := s.RecepcionPorOperacion("op-c")
	if guardada.Clasificacion != ClasificacionCompletada {
		t.Fatalf("clasificación %s != COMPLETADA", guardada.Clasificacion)
	}
}

func TestHisteresisDeteccion(t *testing.T) {
	s := Nuevo()
	if !s.RegistrarDeteccion("ESP32_GESTOCK", true) {
		t.Fatal("la primera detección debería ser nueva")
	}
	if s.RegistrarDeteccion("ESP32_GESTOCK", true) {
		t.Fatal("la detección repetida debería ignorarse (histéresis)")
	}
}

func TestReiniciar(t *testing.T) {
	s := Nuevo()
	_, _, _ = s.Recepcionar(SolicitudRecepcion{CodigoProducto: "PROD-1237", IDOperacion: "op-r"})
	s.Reiniciar()
	if len(s.Recepciones()) != 0 {
		t.Fatal("el historial debería quedar vacío")
	}
	p, _ := s.ProductoPorCodigo("PROD-1237")
	if p.Stock != p.StockInicial {
		t.Fatalf("el stock debería volver a %d, es %d", p.StockInicial, p.Stock)
	}
}
