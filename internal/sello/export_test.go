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

	storeRow := func(series, fecha string) {
		t.Helper()
		if _, err := store.Append(persistence.StoredRecord{
			NIF: "B72877814", Serie: series, Date: fecha, Tipo: "F1",
			Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
			Huella: strings.Repeat(series[len(series)-1:], 1) + strings.Repeat("0", 63-len(series)+1),
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

// TestExportLedgerNeutralizaFormulaInjection: fields that Excel would
// interpret as formulas (=, +, -, @) must leave the agent escaped with a
// leading single quote (audit network/input #3), so a hostile invoice
// series or AEAT field cannot execute when the gestoría opens the CSV.
func TestExportLedgerNeutralizaFormulaInjection(t *testing.T) {
	filas := []persistence.LedgerRow{{
		NIF: "B72877814", Serie: "=1+1", Date: "05-01-2027", Tipo: "F1",
		Cuota: "-21.00", Importe: "121.00", Huella: "aabb", Estado: "pendiente",
		CSV: "@AEAT", Remitido: "no",
	}}
	b, err := ExportLedger(filas)
	if err != nil {
		t.Fatal(err)
	}
	tabla, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(tabla) != 2 {
		t.Fatalf("filas = %d, want 2", len(tabla))
	}
	esperado := map[int]string{0: "'=1+1", 4: "'-21.00", 7: "'@AEAT"}
	for col, want := range esperado {
		if got := tabla[1][col]; got != want {
			t.Fatalf("columna %d = %q, want %q (csv: %v)", col, got, want, tabla[1])
		}
	}
	// clean fields stay untouched
	if tabla[1][2] != "F1" || tabla[1][6] != "pendiente" {
		t.Fatalf("clean fields were altered: %v", tabla[1])
	}
}

// TestExportPeriodoIncluyeClienteModelo303: the tax-advisor CSV carries the
// client NIF and name — model 303 needs the recipient identity, not only
// the emisor totals. The fields travel from the ledger (cliente_nif +
// clientes.nombre) into the two trailing columns, sanitized like every
// other ledger-sourced field.
func TestExportPeriodoIncluyeClienteModelo303(t *testing.T) {
	store, err := persistence.Abrir(t.TempDir() + "/expcliente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Append(persistence.StoredRecord{
		NIF: "B72877814", Serie: "F-2027-0001", Date: "05-01-2027", Tipo: "F1",
		Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
		Huella: strings.Repeat("A", 64), XML: "<x/>",
		ClientNIF: "=B123",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClient("=B123", "@Cliente & Hijos"); err != nil {
		t.Fatal(err)
	}
	srv := &Service{Store: store, EmisorNIF: "B72877814"}
	b, _, err := srv.ExportPeriod("01-01-2027", "31-12-2027")
	if err != nil {
		t.Fatalf("ExportPeriod(): %v", err)
	}
	tabla, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(tabla) != 2 {
		t.Fatalf("filas = %d, want 2", len(tabla))
	}
	if got := tabla[0][len(tabla[0])-2:]; got[0] != "NIFCliente" || got[1] != "NombreCliente" {
		t.Fatalf("cabecera de cliente = %v, want [NIFCliente NombreCliente]", got)
	}
	fila := tabla[1]
	if n := len(fila); n != 12 {
		t.Fatalf("columnas = %d, want 12", n)
	}
	if fila[10] != "'=B123" || fila[11] != "'@Cliente & Hijos" {
		t.Fatalf("cliente = %q/%q, want escapado por inyección de fórmulas", fila[10], fila[11])
	}
}
