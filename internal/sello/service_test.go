package sello

import (
	"context"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// fakeSubmitter acepta todo y devuelve una línea por chainRow del XML.
type fakeSubmitter struct{ recibidos []string }

func (r *fakeSubmitter) Remitir(_ context.Context, soapXML string) (application.RespuestaLote, error) {
	r.recibidos = append(r.recibidos, soapXML)
	lote := application.RespuestaLote{EstadoGlobal: "Correcto", CSV: "CSVSIMULADO00001", TiempoEsperaSeg: 0}
	for _, serie := range extractSeries(soapXML) {
		lote.Lineas = append(lote.Lineas, application.RespuestaLinea{
			NIFEmisor: "B72877814", NumSerie: serie, Fecha: "05-01-2027",
			TipoOperacion: "Alta", Estado: "Correcto"})
	}
	return lote, nil
}

func extractSeries(xml string) []string {
	var out []string
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
		Store: store, Remisor: remisor,
		Flujo:     application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SistemaLocal{
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

	// 2) Envío: control de flujo AEAT = un sobre por ventana de 60 s.
	enviados, err := srv.SubmitPending(context.Background())
	if err != nil {
		t.Fatalf("SubmitPending(): %v", err)
	}
	if enviados != 1 || len(remisor.recibidos) != 1 {
		t.Fatalf("primer ciclo: enviados = %d, remisiones = %d", enviados, len(remisor.recibidos))
	}
	// Antes de 60 s no debe enviar nada más.
	if n, _ := srv.SubmitPending(context.Background()); n != 0 {
		t.Fatalf("segundo ciclo inmediato envió %d, want 0 (espera de flujo)", n)
	}
	reloj.avanzar(61 * time.Second)
	enviados, err = srv.SubmitPending(context.Background())
	if err != nil {
		t.Fatalf("SubmitPending() tras espera: %v", err)
	}
	if enviados != 1 || len(remisor.recibidos) != 2 {
		t.Fatalf("segundo ciclo: enviados = %d, remisiones = %d", enviados, len(remisor.recibidos))
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
			t.Fatalf("fila %+v", f)
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
	srv := &Service{Store: store, Remisor: &fakeSubmitter{},
		Flujo:     application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SistemaLocal{NombreProductor: "p", NIFProductor: "B72877814",
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
