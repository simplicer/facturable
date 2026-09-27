package sello

import (
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
