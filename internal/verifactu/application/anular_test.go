package application

import (
	"strings"
	"testing"
	"time"
)

func TestSellarAnulacionEncadenaYValida(t *testing.T) {
	alta, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, EstadoCadena{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cadena := EstadoCadena{Last: &EnlaceTriple{Serie: alta.Serie, Date: alta.Date, Huella: alta.Huella}}

	res, err := SellarAnulacion("B72877814", "Facturable Test SL", testDraft().Series, "05-01-2027", sisPrueba, cadena, time.Now())
	if err != nil {
		t.Fatalf("SellarAnulacion(): %v", err)
	}
	if res.Tipo != "ANULACION" || res.EnlaceHuella != alta.Huella {
		t.Fatalf("res = %+v", res)
	}
	for _, trozo := range []string{"NumSerieFacturaAnulada", "F-2027-0001", alta.Huella, res.Huella} {
		if !strings.Contains(res.XML, trozo) {
			t.Fatalf("XML sin %q", trozo)
		}
	}
}

func TestSellarAnulacionLibroVacio(t *testing.T) {
	if _, err := SellarAnulacion("B72877814", "X", "X-1", "05-01-2027", sisPrueba, EstadoCadena{}, time.Now()); err == nil {
		t.Fatal("anulación en libro vacío aceptada")
	}
}
