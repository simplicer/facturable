package sello

import (
	"strings"
	"testing"
	"time"
)

func TestQuickInvoiceToDraft(t *testing.T) {
	now := time.Date(2027, 3, 15, 10, 0, 0, 0, time.UTC)
	q := QuickInvoice{ClientName: "ACME SL", ClientNIF: "B44531218", Concept: "Reparación", Base: "100.00"}
	d, err := q.ToDraft("F-2027-0003", now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Series != "F-2027-0004" || d.Date != "15-03-2027" || d.Tipo != "F1" {
		t.Fatalf("defaults = %s %s %s", d.Series, d.Date, d.Tipo)
	}
	if d.Breakdowns[0].Fee != "21.00" || d.Breakdowns[0].Base != "100.00" {
		t.Fatalf("cuota auto = %+v", d.Breakdowns[0])
	}
	if d.ClientNIF != "B44531218" || d.ClientName != "ACME SL" {
		t.Fatalf("cliente = %s %s", d.ClientNIF, d.ClientName)
	}
}

func TestQuickInvoicePrimeraFacturaYCambioAnio(t *testing.T) {
	now := time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
	q := QuickInvoice{ClientName: "X", Concept: "Y", Base: "1.00"}
	d, err := q.ToDraft("F-2026-0450", now) // año anterior: reinicia
	if err != nil || d.Series != "F-2027-0001" {
		t.Fatalf("cambio de año = %s %v", d.Series, err)
	}
}

// TestQuickInvoiceRechazaBaseNoPositiva (audit abuse #3): the quick form
// always emits an ordinary F1 invoice, so a zero or negative base is never
// legal there — a negative base only belongs to an R1-R5 rectificativa,
// which this form cannot produce. The rejection must tell the user why.
func TestQuickInvoiceRechazaBaseNoPositiva(t *testing.T) {
	now := time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
	for _, base := range []string{"0", "0.00", "-0.01", "-5.25"} {
		q := QuickInvoice{ClientName: "X", ClientNIF: "B44531218", Concept: "Y", Base: base}
		_, err := q.ToDraft("", now)
		if err == nil {
			t.Fatalf("base %q aceptada, want rejection", base)
		}
		if !strings.Contains(err.Error(), "mayor que cero") || !strings.Contains(err.Error(), "rectificativa") {
			t.Fatalf("base %q: error poco claro: %v", base, err)
		}
	}
	// the smallest positive base still stamps
	if _, err := (QuickInvoice{ClientName: "X", ClientNIF: "B44531218", Concept: "Y", Base: "0.01"}).ToDraft("", now); err != nil {
		t.Fatalf("base 0.01 rechazada: %v", err)
	}
}

func TestQuickInvoiceIVA4YErrores(t *testing.T) {
	now := time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
	q := QuickInvoice{ClientName: "X", Concept: "Y", Base: "100.00", VATRate: "4"}
	d, err := q.ToDraft("", now)
	if err != nil || d.Breakdowns[0].Fee != "4.00" {
		t.Fatalf("IVA 4%% = %+v %v", d.Breakdowns[0], err)
	}
	if _, err := (QuickInvoice{Concept: "Y", Base: "1"}).ToDraft("", now); err == nil {
		t.Fatal("sin cliente aceptado")
	}
	if _, err := (QuickInvoice{ClientName: "X", Concept: "Y", Base: "abc"}).ToDraft("", now); err == nil {
		t.Fatal("base no numérica aceptada")
	}
}
