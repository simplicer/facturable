package ingest

import (
	"strings"
	"testing"
)

const csvValido = `numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota
F-2027-0001,05-01-2027,F1,Servicios enero,B44531218,ACME SL,21,1000.00,210.00
F-2027-0001,05-01-2027,F1,Servicios enero,B44531218,ACME SL,10,500.00,50.00
F-2027-0002,06-01-2027,F1,Servicios febrero,B44531218,ACME SL,21,2000.00,420.00
`

func TestReadCSVAgrupaBreakdowns(t *testing.T) {
	drafts, errores := ReadCSV(strings.NewReader(csvValido))
	if len(errores) != 0 {
		t.Fatalf("errores inesperados: %v", errores)
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %d, want 2", len(drafts))
	}
	if len(drafts[0].Breakdowns) != 2 || drafts[0].Series != "F-2027-0001" {
		t.Fatalf("primer borrador = %+v", drafts[0])
	}
	if drafts[0].Breakdowns[1].TaxRate != "10" {
		t.Fatalf("desglose 2 = %+v", drafts[0].Breakdowns[1])
	}
}

func TestReadCSVErroresPorFila(t *testing.T) {
	entrada := `numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota
F-2027-0001,05-01-2027,F1,Ok,B44531218,ACME,21,100.00,21.00
MALA,99-99-2027,F1,Date mal,B44531218,ACME,21,100.00,21.00
F-2027-0003,05-01-2027,F1,NIF mal,1234,ACME,21,100.00,21.00
F-2027-0004,05-01-2027,F1,Cuota mal,B44531218,ACME,21,100.00,abc
`
	drafts, errores := ReadCSV(strings.NewReader(entrada))
	if len(drafts) != 1 {
		t.Fatalf("drafts válidos = %d, want 1 (errores: %v)", len(drafts), errores)
	}
	if len(errores) != 3 {
		t.Fatalf("errores = %d, want 3: %v", len(errores), errores)
	}
	// Cada error debe citar row y campo.
	for _, e := range errores {
		if e.Fila == 0 || e.Campo == "" {
			t.Fatalf("error sin localizar: %+v", e)
		}
	}
	if errores[0].Campo != "fecha" || errores[1].Campo != "nif_cliente" || errores[2].Campo != "desglose" {
		t.Fatalf("campos de error = %v %v %v", errores[0].Campo, errores[1].Campo, errores[2].Campo)
	}
}

func TestReadCSVCabeceraIncorrecta(t *testing.T) {
	_, errores := ReadCSV(strings.NewReader("a,b,c\n1,2,3\n"))
	if len(errores) != 1 || errores[0].Campo != "cabecera" {
		t.Fatalf("errores = %v", errores)
	}
}

func TestReadCSVVacio(t *testing.T) {
	_, errores := ReadCSV(strings.NewReader(""))
	if len(errores) != 1 || errores[0].Campo != "cabecera" {
		t.Fatalf("errores = %v", errores)
	}
}
