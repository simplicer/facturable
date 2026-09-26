package sello

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/persistence"
)

func TestExportarPeriodoFiltraYFormatea(t *testing.T) {
	store, err := persistence.Abrir(t.TempDir() + "/exp.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	storeRow := func(serie, fecha string) {
		t.Helper()
		if _, err := store.Append(persistence.StoredRecord{
			NIF: "B72877814", Serie: serie, Fecha: fecha, Tipo: "F1",
			Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
			Huella: strings.Repeat(serie[len(serie)-1:], 1) + strings.Repeat("0", 63-len(serie)+1),
			XML:    "<x/>",
		}); err != nil {
			t.Fatal(err)
		}
	}
	storeRow("F-2027-0001", "05-01-2027")
	storeRow("F-2027-0002", "05-02-2027")
	storeRow("F-2027-0003", "05-03-2027")

	srv := &Service{Store: store, EmisorNIF: "B72877814"}
	b, n, err := srv.ExportPeriod("01-01-2027", "31-01-2027")
	if err != nil {
		t.Fatalf("ExportPeriod(): %v", err)
	}
	if n != 1 {
		t.Fatalf("filas del periodo = %d, want 1", n)
	}
	filas, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(filas) != 2 || filas[1][0] != "F-2027-0001" || filas[1][6] != "pendiente" {
		t.Fatalf("csv = %v", filas)
	}
}

func TestExportarPeriodoValidaFechas(t *testing.T) {
	srv := &Service{}
	if _, _, err := srv.ExportPeriod("31-31-2027", "01-01-2027"); err == nil {
		t.Fatal("fecha inválida aceptada")
	}
	if _, _, err := srv.ExportPeriod("02-01-2027", "01-01-2027"); err == nil {
		t.Fatal("rango invertido aceptado")
	}
	_ = time.Now
}
