package sello

import (
	"context"
	"strings"
	"testing"

	"facturable/internal/sello/ingest"
)

// TestIngestDraftsRechazaAutofactura (audit abuse #2): a draft whose
// recipient NIF is the emisor itself is a self-invoice — the stamping
// pipeline refuses it with the user-facing reason and nothing enters the
// ledger.
func TestIngestDraftsRechazaAutofactura(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	res, err := srv.IngestDrafts([]ingest.Draft{{
		Series: "SELF-0001", Date: "06-01-2027", Tipo: "F1", Description: "obra",
		ClientNIF:  srv.EmisorNIF, // = emisor: autofactura through the stamping door
		ClientName: "Facturable Test SL",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
	}})
	if err != nil {
		t.Fatalf("IngestDrafts(): %v", err)
	}
	if res.Sellados != 0 || len(res.Errores) != 1 {
		t.Fatalf("res = %+v (want 0 sellados, 1 error)", res)
	}
	if !strings.Contains(res.Errores[0].Detalle, "no puedes facturarte a ti mismo") {
		t.Fatalf("detalle = %q, want the self-invoice reason", res.Errores[0].Detalle)
	}
	if dup, _ := srv.Store.HasSeries(srv.EmisorNIF, "SELF-0001"); dup {
		t.Fatal("the self-invoice entered the ledger")
	}
	// submissions stay empty too: a refused draft never becomes a chainRow
	if n, _ := srv.SubmitPending(context.Background()); n != 0 {
		t.Fatalf("SubmitPending() = %d after a refused self-invoice", n)
	}
}

// TestIngestDraftsAceptaClienteDistinto: the guard is exact — a real client
// (different NIF) stamps normally next to the refused self-invoice.
func TestIngestDraftsAceptaClienteDistinto(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	res, err := srv.IngestDrafts([]ingest.Draft{
		{
			Series: "OK-0001", Date: "06-01-2027", Tipo: "F1", Description: "obra",
			ClientNIF: "B44531218", ClientName: "ACME SL",
			Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
		},
		{
			Series: "SELF-0002", Date: "06-01-2027", Tipo: "F1", Description: "obra",
			ClientNIF: srv.EmisorNIF, ClientName: "Facturable Test SL",
			Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
		},
		{
			// empty client NIF is not a self-invoice (records without recipient stay legal)
			Series: "OK-0002", Date: "06-01-2027", Tipo: "F1", Description: "obra",
			Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
		},
	})
	if err != nil {
		t.Fatalf("IngestDrafts(): %v", err)
	}
	if res.Sellados != 2 || len(res.Errores) != 1 {
		t.Fatalf("res = %+v (want 2 sellados, 1 error)", res)
	}
	if res.Errores[0].Fila != 2 {
		t.Fatalf("error fila = %d, want 2 (the self-invoice)", res.Errores[0].Fila)
	}
}

// TestIngestCSVRechazaAutofactura (audit abuse #2): the CSV batch door gets
// the same guard — one self-invoice row fails with the reason, the rest of
// the batch stamps on.
func TestIngestCSVRechazaAutofactura(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	csv := "numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota\n" +
		"F-2027-0001,05-01-2027,F1,Servicios enero,B44531218,ACME SL,21,1000.00,210.00\n" +
		"F-2027-0002,05-01-2027,F1,autofactura," + srv.EmisorNIF + ",Yo mismo,21,100.00,21.00\n"
	res, err := srv.IngestCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("IngestCSV(): %v", err)
	}
	if res.Sellados != 1 || len(res.Errores) != 1 {
		t.Fatalf("res = %+v (want 1 sellado, 1 error)", res)
	}
	if !strings.Contains(res.Errores[0].Detalle, "no puedes facturarte a ti mismo") {
		t.Fatalf("detalle = %q, want the self-invoice reason", res.Errores[0].Detalle)
	}
}
