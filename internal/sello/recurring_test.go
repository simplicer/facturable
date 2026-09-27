package sello

import (
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/ingest"
)

func TestRecurringBatchStampAll(t *testing.T) {
	seedOne := []ingest.Draft{{
		Series: "F-2027-0001", Date: "01-01-2027", Tipo: "F1", Description: "Ya en el libro",
		ClientNIF: "B44531218", ClientName: "ACME SL",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "500.00", Fee: "105.00"}},
	}}

	cases := []struct {
		name         string
		seed         []ingest.Draft
		clients      []RecurringClient
		wantSelladas int
		wantFallidas int
		wantSeries   []string // one per client, in order; "" = expected failure
		wantFecha    string   // expedition date of every stamped invoice
		wantLedger   int
	}{
		{
			name: "stamps the whole list in sequence",
			clients: []RecurringClient{
				{ClientName: "ACME SL", ClientNIF: "B44531218", Concept: "Mantenimiento mensual", Base: "500.00"},
				{ClientName: "Zar S.L.", ClientNIF: "B95686044", Concept: "Alquiler nave", Base: "300.00", VATRate: "10"},
			},
			wantSelladas: 2, wantFallidas: 0,
			wantSeries: []string{"F-2027-0001", "F-2027-0002"}, wantFecha: "05-01-2027",
			wantLedger: 2,
		},
		{
			name: "one bad client does not abort the batch",
			clients: []RecurringClient{
				{ClientName: "ACME SL", ClientNIF: "B44531218", Concept: "Mantenimiento", Base: "500.00"},
				{ClientName: "Importe Roto SL", ClientNIF: "B95686044", Concept: "Roto", Base: "abc"},
				{ClientName: "Última SL", ClientNIF: "B44531218", Concept: "Cuota gym", Base: "45.50", VATRate: "21"},
			},
			wantSelladas: 2, wantFallidas: 1,
			wantSeries: []string{"F-2027-0001", "", "F-2027-0002"}, wantFecha: "05-01-2027",
			wantLedger: 2,
		},
		{
			name: "continues the series of the existing ledger",
			seed: seedOne,
			clients: []RecurringClient{
				{ClientName: "Zar S.L.", ClientNIF: "B95686044", Concept: "Alquiler nave", Base: "300.00", VATRate: "10"},
			},
			wantSelladas: 1, wantFallidas: 0,
			wantSeries: []string{"F-2027-0002"}, wantFecha: "05-01-2027",
			wantLedger: 2,
		},
		{
			name: "empty batch stamps nothing", clients: nil,
			wantSelladas: 0, wantFallidas: 0, wantSeries: nil, wantLedger: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, reloj, _ := newTestService(t)
			srv.Now = reloj.ahora // today = the fake clock instant
			reloj.t = time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
			if len(tc.seed) > 0 {
				if _, err := srv.IngestDrafts(tc.seed); err != nil {
					t.Fatal(err)
				}
			}

			res := NewRecurringBatch(srv, tc.clients).StampAll()

			if res.Selladas != tc.wantSelladas || res.Fallidas != tc.wantFallidas {
				t.Fatalf("resumen = selladas %d, fallidas %d (want %d/%d): %+v",
					res.Selladas, res.Fallidas, tc.wantSelladas, tc.wantFallidas, res.Facturas)
			}
			if len(res.Facturas) != len(tc.clients) {
				t.Fatalf("una línea por cliente: hay %d, want %d", len(res.Facturas), len(tc.clients))
			}
			for i, f := range res.Facturas {
				if f.Serie != tc.wantSeries[i] {
					t.Fatalf("factura %d serie = %q, want %q", i, f.Serie, tc.wantSeries[i])
				}
				if (f.Error != "") != (f.Serie == "") {
					t.Fatalf("factura %d = %+v (serie o error, no ambos ni ninguno)", i, f)
				}
			}
			filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(filas) != tc.wantLedger {
				t.Fatalf("libro = %d filas, want %d", len(filas), tc.wantLedger)
			}
			for _, f := range filas[len(filas)-res.Selladas:] {
				if f.Date != tc.wantFecha {
					t.Fatalf("fecha de %s = %s, want hoy %s", f.Serie, f.Date, tc.wantFecha)
				}
			}
		})
	}
}

func TestRecurringBatchReportsPerClientError(t *testing.T) {
	srv, _, reloj, _ := newTestService(t)
	srv.Now = reloj.ahora
	res := NewRecurringBatch(srv, []RecurringClient{
		{ClientName: "Importe Roto SL", ClientNIF: "B95686044", Concept: "Roto", Base: "no-soy-un-número"},
	}).StampAll()
	if res.Fallidas != 1 || res.Facturas[0].Error == "" {
		t.Fatalf("error por cliente no reportado: %+v", res)
	}
	if !strings.Contains(res.Facturas[0].Error, "base") {
		t.Fatalf("detalle sin campo: %q", res.Facturas[0].Error)
	}
	if res.Facturas[0].Serie != "" {
		t.Fatalf("factura fallida con serie: %+v", res.Facturas[0])
	}
}

func TestRecurringBatchChainsHealthy(t *testing.T) {
	srv, _, reloj, _ := newTestService(t)
	srv.Now = reloj.ahora
	res := NewRecurringBatch(srv, []RecurringClient{
		{ClientName: "ACME SL", ClientNIF: "B44531218", Concept: "Mantenimiento mensual", Base: "500.00"},
		{ClientName: "Zar S.L.", ClientNIF: "B95686044", Concept: "Alquiler nave", Base: "300.00", VATRate: "10"},
	}).StampAll()
	if res.Selladas != 2 {
		t.Fatalf("resumen = %+v", res)
	}
	// las dos facturas selladas en secuencia encadenan sin rupturas
	info, err := srv.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Healthy() || info.Registros != 2 {
		t.Fatalf("cadena sana=%v registros=%d rupturas=%v", info.Healthy(), info.Registros, info.Rupturas)
	}
}
