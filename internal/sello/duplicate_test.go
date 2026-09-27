package sello

import (
	"errors"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/ingest"
)

// draftUUID returns the public UUID of the record with that series.
func draftUUID(t *testing.T, srv *Service, serie string) string {
	t.Helper()
	filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range filas {
		if f.Serie == serie {
			return f.UUID
		}
	}
	t.Fatalf("no existe %s en el libro", serie)
	return ""
}

func TestQuickInvoiceFromXML(t *testing.T) {
	completo := `<sum1:RegistroAlta>
	<sum1:DescripcionOperacion>Mensualidad enero</sum1:DescripcionOperacion>
	<sum1:Destinatarios>
		<sum1:IDDestinatario>
			<sum1:NombreRazon>ACME SL</sum1:NombreRazon>
			<sum1:NIF>B44531218</sum1:NIF>
		</sum1:IDDestinatario>
	</sum1:Destinatarios>
	<sum1:Desglose>
		<sum1:DetalleDesglose>
			<sum1:TipoImpositivo>21</sum1:TipoImpositivo>
			<sum1:BaseImponibleOimporteNoSujeto>500.00</sum1:BaseImponibleOimporteNoSujeto>
			<sum1:CuotaRepercutida>105.00</sum1:CuotaRepercutida>
		</sum1:DetalleDesglose>
	</sum1:Desglose>
</sum1:RegistroAlta>`
	cases := []struct {
		name string
		xml  string
		want QuickInvoice
	}{
		{
			name: "recovers the five quick-form fields",
			xml:  completo,
			want: QuickInvoice{ClientName: "ACME SL", ClientNIF: "B44531218",
				Concept: "Mensualidad enero", Base: "500.00", VATRate: "21"},
		},
		{
			name: "without recipient the client fields stay empty",
			xml:  "<sum1:RegistroAlta><sum1:DescripcionOperacion>x</sum1:DescripcionOperacion></sum1:RegistroAlta>",
			want: QuickInvoice{Concept: "x"},
		},
		{
			name: "without breakdown base and rate stay empty",
			xml: "<sum1:RegistroAlta><sum1:Destinatarios><sum1:IDDestinatario>" +
				"<sum1:NombreRazon>Zar S.L.</sum1:NombreRazon></sum1:IDDestinatario></sum1:Destinatarios></sum1:RegistroAlta>",
			want: QuickInvoice{ClientName: "Zar S.L."},
		},
		{
			name: "empty xml yields the zero quick form",
			xml:  "",
			want: QuickInvoice{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := QuickInvoiceFromXML(tc.xml); got != tc.want {
				t.Fatalf("QuickInvoiceFromXML() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDuplicateInvoice(t *testing.T) {
	today := time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)
	goodSeed := []ingest.Draft{{
		Series: "F-2027-0001", Date: "01-01-2027", Tipo: "F1", Description: "Mensualidad enero",
		ClientNIF: "B44531218", ClientName: "ACME SL",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "500.00", Fee: "105.00"}},
	}}
	rate10Seed := []ingest.Draft{{
		Series: "F-2027-0001", Date: "02-01-2027", Tipo: "F1", Description: "Alquiler nave",
		ClientNIF: "B95686044", ClientName: "Zar S.L.",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "10", Base: "300.00", Fee: "30.00"}},
	}}
	withoutClientSeed := []ingest.Draft{{
		Series: "F-2027-0001", Date: "02-01-2027", Tipo: "F1", Description: "Cliente anónimo",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "100.00", Fee: "21.00"}},
	}}

	cases := []struct {
		name         string
		seed         []ingest.Draft
		uuidOf       string // series whose uuid gets duplicated; "" = unknown uuid
		wantSerie    string
		wantFecha    string
		wantCuota    string
		wantTotal    string
		wantOriginal string
		wantLedger   int
		wantErrPart  string // substring of the error; "" = no error
		wantNotFound bool
	}{
		{
			name: "duplicates with today's date and a fresh series",
			seed: goodSeed, uuidOf: "F-2027-0001",
			wantSerie: "F-2027-0002", wantFecha: "05-01-2027",
			wantCuota: "105.00", wantTotal: "605.00", wantOriginal: "F-2027-0001",
			wantLedger: 2,
		},
		{
			name: "recovers client, concept and breakdown from the stored XML",
			seed: rate10Seed, uuidOf: "F-2027-0001",
			wantSerie: "F-2027-0002", wantFecha: "05-01-2027",
			wantCuota: "30.00", wantTotal: "330.00", wantOriginal: "F-2027-0001",
			wantLedger: 2,
		},
		{
			name: "unknown uuid is not found", uuidOf: "",
			wantErrPart: "no existe el registro solicitado", wantNotFound: true, wantLedger: 0,
		},
		{
			name: "invoice without a stored client fails without stamping",
			seed: withoutClientSeed, uuidOf: "F-2027-0001",
			wantErrPart: "falta el nombre del cliente", wantLedger: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, reloj, _ := newTestService(t)
			srv.Now = reloj.ahora // today = the fake clock instant
			reloj.t = today
			if len(tc.seed) > 0 {
				if _, err := srv.IngestDrafts(tc.seed); err != nil {
					t.Fatal(err)
				}
			}
			uuid := ""
			if tc.uuidOf != "" {
				uuid = draftUUID(t, srv, tc.uuidOf)
			}
			res, err := srv.DuplicateInvoice(uuid)

			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("DuplicateInvoice() = %+v, want error %q", res, tc.wantErrPart)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErrPart)
				}
				if tc.wantNotFound && !errors.Is(err, ErrRecordNotFound) {
					t.Fatalf("error = %v, want ErrRecordNotFound", err)
				}
			} else if err != nil {
				t.Fatalf("DuplicateInvoice(): %v", err)
			}
			if tc.wantErrPart == "" {
				if res.Serie != tc.wantSerie || res.Fecha != tc.wantFecha ||
					res.Cuota != tc.wantCuota || res.Total != tc.wantTotal || res.Original != uuid {
					t.Fatalf("duplicada = %+v", res)
				}
				if !res.Sellada {
					t.Fatalf("sellada = false: %+v", res)
				}
				// la copia mantiene cliente y concepto del original
				filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 100)
				if err != nil {
					t.Fatal(err)
				}
				if len(filas) != tc.wantLedger {
					t.Fatalf("libro = %d filas, want %d", len(filas), tc.wantLedger)
				}
			}
		})
	}
}

func TestDuplicateInvoiceKeepsClientAndConcept(t *testing.T) {
	srv, _, reloj, _ := newTestService(t)
	srv.Now = reloj.ahora
	seed := []ingest.Draft{{
		Series: "F-2027-0001", Date: "01-01-2027", Tipo: "F1", Description: "Mensualidad enero",
		ClientNIF: "B44531218", ClientName: "ACME SL",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "500.00", Fee: "105.00"}},
	}}
	if _, err := srv.IngestDrafts(seed); err != nil {
		t.Fatal(err)
	}
	res, err := srv.DuplicateInvoice(draftUUID(t, srv, "F-2027-0001"))
	if err != nil {
		t.Fatal(err)
	}
	filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 100)
	if err != nil || len(filas) != 2 {
		t.Fatalf("libro = %+v err=%v", filas, err)
	}
	copia := filas[1]
	if copia.Serie != res.Serie || copia.Importe != "605.00" || copia.Cuota != "105.00" {
		t.Fatalf("copia en el libro = %+v", copia)
	}
	// la cadena sigue sana: la copia encadena con la original
	info, err := srv.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Healthy() || info.Registros != 2 {
		t.Fatalf("cadena sana=%v registros=%d rupturas=%v", info.Healthy(), info.Registros, info.Rupturas)
	}
}

func TestDuplicateInvoiceSeriesAdvance(t *testing.T) {
	srv, _, reloj, _ := newTestService(t)
	srv.Now = reloj.ahora
	seed := []ingest.Draft{{
		Series: "F-2027-0001", Date: "01-01-2027", Tipo: "F1", Description: "Mensualidad enero",
		ClientNIF: "B44531218", ClientName: "ACME SL",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "500.00", Fee: "105.00"}},
	}}
	if _, err := srv.IngestDrafts(seed); err != nil {
		t.Fatal(err)
	}
	r1, err := srv.DuplicateInvoice(draftUUID(t, srv, "F-2027-0001"))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Serie != "F-2027-0002" {
		t.Fatalf("primera duplicada = %s, want F-2027-0002", r1.Serie)
	}
	// duplicar sobre la duplicada: la serie vuelve a avanzar
	r2, err := srv.DuplicateInvoice(draftUUID(t, srv, "F-2027-0002"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Serie != "F-2027-0003" {
		t.Fatalf("segunda duplicada = %s, want F-2027-0003", r2.Serie)
	}
}
