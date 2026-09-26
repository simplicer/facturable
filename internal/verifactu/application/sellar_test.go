package application

import (
	"strings"
	"testing"
	"time"
)

var sisPrueba = SistemaLocal{
	NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
	NombreSistema: "Facturable Sello", IDSistema: "FS",
	Version: "0.1.0", NumeroInstalacion: "00001",
}

func testDraft() BorradorAlta {
	return BorradorAlta{
		NumeroSerie: "F-2027-0001", Fecha: "05-01-2027", Tipo: "F1",
		Descripcion: "Servicios de enero", NIFCliente: "B44531218", NombreCliente: "ACME SL",
		Desgloses: []DesgloseAlta{
			{TipoImpositivo: "21", Base: "1000.00", Cuota: "210.00"},
			{TipoImpositivo: "10", Base: "500.00", Cuota: "50.00"},
		},
	}
}

func TestSellarAltaPrimeraFactura(t *testing.T) {
	ahora := time.Date(2027, 1, 5, 10, 0, 0, 0, madridTZ(t))
	res, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, EstadoCadena{}, ahora)
	if err != nil {
		t.Fatalf("SellarAlta() = %v", err)
	}
	if !res.EsPrimero || res.EnlaceHuella != "" {
		t.Fatalf("debería ser el primer chainRow: %+v", res)
	}
	if res.Cuota != "260.00" || res.Importe != "1760.00" {
		t.Fatalf("totales = %s / %s, want 260.00 / 1760.00", res.Cuota, res.Importe)
	}
	if len(res.Huella) != 64 || res.Huella != strings.ToUpper(res.Huella) {
		t.Fatalf("huella mal formada: %q", res.Huella)
	}
	for _, trozo := range []string{"PrimerRegistro", "IDVersion", "F-2027-0001", "B44531218", "CuotaTotal", "TipoHuella", res.Huella} {
		if !strings.Contains(res.XML, trozo) {
			t.Fatalf("el XML no contiene %q", trozo)
		}
	}
}

func TestSellarAltaEncadenaConAnterior(t *testing.T) {
	primera, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, EstadoCadena{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	segundaB := testDraft()
	segundaB.NumeroSerie = "F-2027-0002"

	cadena := EstadoCadena{Last: &EnlaceTriple{Serie: primera.Serie, Fecha: primera.Fecha, Huella: primera.Huella}}
	res, err := SellarAlta("B72877814", "Facturable Test SL", segundaB, sisPrueba, cadena, time.Now())
	if err != nil {
		t.Fatalf("SellarAlta() = %v", err)
	}
	if res.EsPrimero || res.EnlaceHuella != primera.Huella {
		t.Fatalf("encadenamiento incorrecto: %+v", res)
	}
	if !strings.Contains(res.XML, primera.Huella) {
		t.Fatal("el XML debe incrustar la huella anterior")
	}
}

func TestSellarAltaRechazaCadenaRota(t *testing.T) {
	primera, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, EstadoCadena{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// n-1 declara un n-2 distinto del n-2 real almacenado.
	declarado := &EnlaceTriple{Serie: "F-2026-9999", Fecha: "31-12-2026", Huella: strings.Repeat("E", 64)}
	real := &EnlaceTriple{Serie: "F-2026-9998", Fecha: "30-12-2026", Huella: strings.Repeat("D", 64)}
	cadena := EstadoCadena{
		Last:               &EnlaceTriple{Serie: primera.Serie, Fecha: primera.Fecha, Huella: primera.Huella},
		DeclaradoPorUltimo: declarado,
		Previous:           real,
	}
	if _, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, cadena, time.Now()); err == nil {
		t.Fatal("SellarAlta() = nil, want cadena rota (declarado != real)")
	}

	// Caso válido: lo declarado coincide con el real.
	cadenaOK := EstadoCadena{
		Last:               &EnlaceTriple{Serie: primera.Serie, Fecha: primera.Fecha, Huella: primera.Huella},
		DeclaradoPorUltimo: real,
		Previous:           real,
	}
	if _, err := SellarAlta("B72877814", "Facturable Test SL", testDraft(), sisPrueba, cadenaOK, time.Now()); err != nil {
		t.Fatalf("SellarAlta() con cadena válida = %v", err)
	}
}

func TestSellarAltaErroresDato(t *testing.T) {
	base := testDraft()

	b := base
	b.NumeroSerie = ""
	if _, err := SellarAlta("B72877814", "X", b, sisPrueba, EstadoCadena{}, time.Now()); err == nil {
		t.Fatal("serie vacía aceptada")
	}

	b = base
	b.NIFCliente = "1234"
	if _, err := SellarAlta("B72877814", "X", b, sisPrueba, EstadoCadena{}, time.Now()); err == nil {
		t.Fatal("NIF cliente inválido aceptado")
	}

	b = base
	b.Desgloses = nil
	if _, err := SellarAlta("B72877814", "X", b, sisPrueba, EstadoCadena{}, time.Now()); err == nil {
		t.Fatal("sin desgloses aceptado")
	}

	b = base
	b.Desgloses = []DesgloseAlta{{TipoImpositivo: "21", Base: "1.00", Cuota: "0,21"}}
	res, err := SellarAlta("B72877814", "X", b, sisPrueba, EstadoCadena{}, time.Now())
	if err != nil || res.Cuota != "0.21" || res.Importe != "1.21" {
		t.Fatalf("coma decimal: res=%+v err=%v", res, err)
	}
}

func madridTZ(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
