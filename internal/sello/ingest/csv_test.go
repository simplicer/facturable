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

func TestLeerCSVAgrupaDesgloses(t *testing.T) {
	borradores, errores := LeerCSV(strings.NewReader(csvValido))
	if len(errores) != 0 {
		t.Fatalf("errores inesperados: %v", errores)
	}
	if len(borradores) != 2 {
		t.Fatalf("borradores = %d, want 2", len(borradores))
	}
	if len(borradores[0].Desgloses) != 2 || borradores[0].NumeroSerie != "F-2027-0001" {
		t.Fatalf("primer borrador = %+v", borradores[0])
	}
	if borradores[0].Desgloses[1].TipoImpositivo != "10" {
		t.Fatalf("desglose 2 = %+v", borradores[0].Desgloses[1])
	}
}

func TestLeerCSVErroresPorFila(t *testing.T) {
	entrada := `numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota
F-2027-0001,05-01-2027,F1,Ok,B44531218,ACME,21,100.00,21.00
MALA,99-99-2027,F1,Fecha mal,B44531218,ACME,21,100.00,21.00
F-2027-0003,05-01-2027,F1,NIF mal,1234,ACME,21,100.00,21.00
F-2027-0004,05-01-2027,F1,Cuota mal,B44531218,ACME,21,100.00,abc
`
	borradores, errores := LeerCSV(strings.NewReader(entrada))
	if len(borradores) != 1 {
		t.Fatalf("borradores válidos = %d, want 1 (errores: %v)", len(borradores), errores)
	}
	if len(errores) != 3 {
		t.Fatalf("errores = %d, want 3: %v", len(errores), errores)
	}
	// Cada error debe citar fila y campo.
	for _, e := range errores {
		if e.Fila == 0 || e.Campo == "" {
			t.Fatalf("error sin localizar: %+v", e)
		}
	}
	if errores[0].Campo != "fecha" || errores[1].Campo != "nif_cliente" || errores[2].Campo != "desglose" {
		t.Fatalf("campos de error = %v %v %v", errores[0].Campo, errores[1].Campo, errores[2].Campo)
	}
}

func TestLeerCSVCabeceraIncorrecta(t *testing.T) {
	_, errores := LeerCSV(strings.NewReader("a,b,c\n1,2,3\n"))
	if len(errores) != 1 || errores[0].Campo != "cabecera" {
		t.Fatalf("errores = %v", errores)
	}
}

func TestLeerCSVVacio(t *testing.T) {
	_, errores := LeerCSV(strings.NewReader(""))
	if len(errores) != 1 || errores[0].Campo != "cabecera" {
		t.Fatalf("errores = %v", errores)
	}
}
