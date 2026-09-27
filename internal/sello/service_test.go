package sello

import (
	"context"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/ingest"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// fakeSubmitter acepta todo y devuelve una línea por chainRow del XML.
type fakeSubmitter struct{ recibidos []string }

func (r *fakeSubmitter) Submit(_ context.Context, soapXML string) (application.RespuestaLote, error) {
	r.recibidos = append(r.recibidos, soapXML)
	lote := application.RespuestaLote{EstadoGlobal: "Correcto", CSV: "CSVSIMULADO00001", TiempoEsperaSeg: 0}
	for _, series := range extractSeries(soapXML) {
		lote.Lineas = append(lote.Lineas, application.RespuestaLinea{
			NIFEmisor: "B72877814", NumSerie: series, Date: "05-01-2027",
			TipoOperacion: "Alta", Estado: "Correcto"})
	}
	return lote, nil
}

func extractSeries(xml string) []string {
	var out []string
	for _, trozo := range strings.Split(xml, "<sum1:NumSerieFacturaAnulada>") {
		if i := strings.Index(trozo, "</sum1:NumSerieFacturaAnulada>"); i > 0 && !strings.Contains(trozo[:i], "<") {
			out = append(out, trozo[:i])
		}
	}
	for _, trozo := range strings.Split(xml, "<sum1:NumSerieFactura>") {
		if i := strings.Index(trozo, "</sum1:NumSerieFactura>"); i > 0 && !strings.Contains(trozo[:i], "<") {
			out = append(out, trozo[:i])
		}
	}
	// elimina duplicados (factura + encadenamiento)
	vistos := map[string]bool{}
	limpio := out[:0]
	for _, s := range out {
		if !vistos[s] {
			vistos[s] = true
			limpio = append(limpio, s)
		}
	}
	return limpio
}

const csvPrueba = `numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota
F-2027-0001,05-01-2027,F1,Servicios enero,B44531218,ACME SL,21,1000.00,210.00
F-2027-0002,05-01-2027,F1,Servicios enero 2,B44531218,ACME SL,21,500.00,105.00
`

// relojFalso permite avanzar el tiempo del control de flujo en pruebas.
type relojFalso struct{ t time.Time }

func (r *relojFalso) ahora() time.Time        { return r.t }
func (r *relojFalso) avanzar(d time.Duration) { r.t = r.t.Add(d) }

func TestCircuitoCompleto(t *testing.T) {
	store, err := persistence.Abrir(t.TempDir() + "/agente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reloj := &relojFalso{t: time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)}
	remisor := &fakeSubmitter{}
	srv := &Service{
		Store: store, Submitter: remisor,
		Flujo:     application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SystemInfo{
			NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
			NombreSistema: "Facturable Sello", IDSistema: "FS",
			Version: "0.1.0", NumeroInstalacion: "00001"},
	}

	// 1) Ingesta: dos facturas selladas y encadenadas.
	res, err := srv.IngestCSV(strings.NewReader(csvPrueba))
	if err != nil {
		t.Fatalf("IngestCSV(): %v", err)
	}
	if res.Sellados != 2 || len(res.Errores) != 0 {
		t.Fatalf("ingesta = %+v", res)
	}

	// 2) Envío en LOTE: ambos pendientes caben en un sobre (≤1000, spec).
	enviados, err := srv.SubmitPending(context.Background())
	if err != nil {
		t.Fatalf("SubmitPending(): %v", err)
	}
	if enviados != 2 || len(remisor.recibidos) != 1 {
		t.Fatalf("primer ciclo: enviados = %d, sobres = %d (want 2/1)", enviados, len(remisor.recibidos))
	}
	// Sin pendientes: aunque pase la ventana, no envía.
	reloj.avanzar(61 * time.Second)
	if n, _ := srv.SubmitPending(context.Background()); n != 0 {
		t.Fatalf("sin pendientes envió %d", n)
	}

	// 3) El libro refleja estado correcto y CSV persistido.
	filas, err := store.Ledger("B72877814", 0, 1_000_000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(filas) != 2 {
		t.Fatalf("libro = %d filas", len(filas))
	}
	for _, f := range filas {
		if f.Estado != "correcto" || f.CSV != "CSVSIMULADO00001" {
			t.Fatalf("row %+v", f)
		}
	}

	// 4) Ya no queda nada pendiente.
	pend, err := store.Pending("B72877814", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 0 {
		t.Fatalf("pendientes = %d", len(pend))
	}
}

func TestCircuitoIngestaConErroresNoRompeLibro(t *testing.T) {
	store, err := persistence.Abrir(t.TempDir() + "/agente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reloj := &relojFalso{t: time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)}
	srv := &Service{Store: store, Submitter: &fakeSubmitter{},
		Flujo:     application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SystemInfo{NombreProductor: "p", NIFProductor: "B72877814",
			NombreSistema: "s", IDSistema: "FS", Version: "0", NumeroInstalacion: "1"}}

	res, err := srv.IngestCSV(strings.NewReader("numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota\n F-2027-0009,99-99-2027,F1,x,B44531218,ACME,21,10.00,2.10\n"))
	if err != nil {
		t.Fatalf("IngestCSV(): %v", err)
	}
	if res.Sellados != 0 || len(res.Errores) != 1 {
		t.Fatalf("res = %+v", res)
	}
	pend, _ := store.Pending("B72877814", 10)
	if len(pend) != 0 {
		t.Fatal("un borrador inválido no debe entrar en el libro")
	}
}

// La memoria de clientes se alimenta sola: cada registro sellado con
// NIF+nombre de cliente (por CSV o por borrador) queda recordado, y los
// registros sin cliente no ensucian la memoria.
func TestIngestAlimentaMemoriaClientes(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	if _, err := srv.IngestCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatalf("IngestCSV(): %v", err)
	}
	conCliente, err := srv.IngestDrafts([]ingest.Draft{{
		Series: "DRA-0001", Date: "06-01-2027", Tipo: "F1", Description: "obra",
		ClientNIF: "12345678Z", ClientName: "Juan Pérez",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
	}})
	if err != nil || conCliente.Sellados != 1 {
		t.Fatalf("IngestDrafts() = %+v %v", conCliente, err)
	}
	sinCliente, err := srv.IngestDrafts([]ingest.Draft{{
		Series: "DRA-0002", Date: "06-01-2027", Tipo: "F1", Description: "obra",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
	}})
	if err != nil || sinCliente.Sellados != 1 {
		t.Fatalf("IngestDrafts() sin cliente = %+v %v", sinCliente, err)
	}

	got, err := srv.Store.FrequentClients(10)
	if err != nil {
		t.Fatal(err)
	}
	want := []persistence.FrequentClient{
		{NIF: "B44531218", Nombre: "ACME SL", Usos: 2},
		{NIF: "12345678Z", Nombre: "Juan Pérez", Usos: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("FrequentClients = %+v (want %+v)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FrequentClients[%d] = %+v (want %+v)", i, got[i], want[i])
		}
	}
}

func TestIngestDraftsDedupePorSerie(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	d := []ingest.Draft{{
		Series: "DUP-0001", Date: "05-01-2027", Tipo: "F1", Description: "x",
		Breakdowns: []ingest.BreakdownDraft{{TaxRate: "21", Base: "10.00", Fee: "2.10"}},
	}}
	r1, err := srv.IngestDrafts(d)
	if err != nil || r1.Sellados != 1 {
		t.Fatalf("primera ingesta = %+v %v", r1, err)
	}
	r2, err := srv.IngestDrafts(d)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Sellados != 0 || len(r2.Errores) != 1 {
		t.Fatalf("webhook repetido = %+v (want 0 sellados, 1 aviso)", r2)
	}
}
