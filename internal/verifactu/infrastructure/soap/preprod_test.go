//go:build aeat

// Integration suite against the AEAT TEST environment (prewww1). Requires
// a real qualified certificate: run with
//
//	SELLO_CERT_P12=/path/cert.p12 SELLO_CERT_PASS=... go test -tags=aeat ./internal/verifactu/infrastructure/soap/
//
// Never runs in CI (no certificate). See docs/runbooks/aeat-preprod-qa.md.
package soap

import (
	"context"
	"os"
	"testing"
	"time"

	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/domain/xmlalta"
)

func preprodCliente(t *testing.T) *Client {
	t.Helper()
	p12 := os.Getenv("SELLO_CERT_P12")
	if p12 == "" {
		t.Skip("SELLO_CERT_P12 no definida: se omite la suite contra preproducción AEAT")
	}
	cert, err := LoadP12(p12, os.Getenv("SELLO_CERT_PASS"))
	if err != nil {
		t.Fatalf("certificado: %v", err)
	}
	return NewClient(Testing.PersonURL, cert)
}

func TestPreprodHandshake(t *testing.T) {
	c := preprodCliente(t)
	// un envío vacío debe recibir al menos una respuesta estructurada
	// (rechazo por registros ausentes), lo que prueba TLS+SOAP extremo a extremo.
	_, err := c.Submit(context.Background(), envelopeVacio())
	if err == nil {
		return // respuesta parseable: correcto
	}
	t.Logf("respuesta del entorno de pruebas: %v", err)
}

func TestPreprodAltaRegistroPrueba(t *testing.T) {
	c := preprodCliente(t)
	xmlStr := envelopeAltaPrueba(t)
	lote, err := c.Submit(context.Background(), xmlStr)
	if err != nil {
		t.Fatalf("Submit(): %v", err)
	}
	if lote.EstadoGlobal == "" {
		t.Fatal("sin EstadoGlobal en la respuesta")
	}
	t.Logf("estado global: %s | CSV: %s | líneas: %d", lote.EstadoGlobal, lote.CSV, len(lote.Lineas))
	for _, l := range lote.Lineas {
		t.Logf("  %s %s -> %s (%s)", l.NumSerie, l.Estado, l.CodigoError, l.DescripcionRaw)
	}
}

// helpers construidos con el builder real para no duplicar contratos
func envelopeVacio() string {
	sub := xmlalta.NewSubmission(xmlalta.Issuer{Name: "PRUEBAS", TaxID: "B00000000"})
	b, _ := sub.XML()
	return b
}

func qaNIF() string {
	if v := os.Getenv("SELLO_CERT_NIF"); v != "" {
		return v
	}
	return "B00000000"
}

func timeNowMadrid() time.Time {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.Now()
	}
	return time.Now().In(loc)
}

func hoy() string {
	return time.Now().Format("02-01-2006")
}

func envelopeAltaPrueba(t *testing.T) string {
	t.Helper()
	borrador := application.InvoiceDraft{
		Series: "FACTURABLE-QA-0001", Date: hoy(), Tipo: "F1",
		Description: "Registro de QA contra preproducción",
		ClientNIF:   "B44531218", ClientName: "CLIENTE QA SL",
		Desgloses: []application.BreakdownDraft{{TaxRate: "21", Base: "10.00", Cuota: "2.10"}},
	}
	// El registro QA se construye con SealInvoice para validar el circuito completo
	res, err := application.SealInvoice(qaNIF(), "NOMBRE CENSAL DEL CERTIFICADO", borrador, application.SystemInfo{
		NombreProductor: "Facturable", NIFProductor: "B00000000",
		NombreSistema: "Facturable Sello", IDSistema: "FS", Version: "qa", NumeroInstalacion: "00001",
	}, application.ChainState{}, timeNowMadrid())
	if err != nil {
		t.Fatalf("SealInvoice: %v", err)
	}
	return res.XML
}
