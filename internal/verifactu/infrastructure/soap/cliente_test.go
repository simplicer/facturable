package soap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestClient levanta un servidor que responde la respuesta AEAT de
// ejemplo y devuelve un Cliente apuntando a él.
func newTestClient(t *testing.T, status int, body string) (*Cliente, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/soap+xml; charset=utf-8" {
			t.Errorf("Content-Type = %q", ct)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewCliente(srv.URL, tls.Certificate{}), srv
}

const okResponse = `<?xml version="1.0"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
 <soapenv:Body>
  <sf:RespuestaRegFactuSistemaFacturacion xmlns:sf="urn:test">
   <sf:CSV>ABC123DEF456GH78</sf:CSV>
   <sf:TiempoEsperaEnvio>60</sf:TiempoEsperaEnvio>
   <sf:EstadoEnvio>Correcto</sf:EstadoEnvio>
   <sf:RespuestaLinea>
    <sf:IDFactura>
     <sf:IDEmisorFactura>B72877814</sf:IDEmisorFactura>
     <sf:NumSerieFactura>F-2027-0001</sf:NumSerieFactura>
     <sf:FechaExpedicionFactura>05-01-2027</sf:FechaExpedicionFactura>
    </sf:IDFactura>
    <sf:TipoOperacion>Alta</sf:TipoOperacion>
    <sf:EstadoRegistro>Correcto</sf:EstadoRegistro>
   </sf:RespuestaLinea>
  </sf:RespuestaRegFactuSistemaFacturacion>
 </soapenv:Body>
</soapenv:Envelope>`

func TestRemitirOK(t *testing.T) {
	c, _ := newTestClient(t, http.StatusOK, okResponse)
	lote, err := c.Remitir(context.Background(), "<envelope/>")
	if err != nil {
		t.Fatalf("Remitir() = %v", err)
	}
	if lote.CSV != "ABC123DEF456GH78" || lote.EstadoGlobal != "Correcto" || len(lote.Lineas) != 1 {
		t.Fatalf("lote = %+v", lote)
	}
}

func TestRemitirHTTPError(t *testing.T) {
	c, _ := newTestClient(t, http.StatusInternalServerError, "boom")
	_, err := c.Remitir(context.Background(), "<envelope/>")
	if err == nil {
		t.Fatal("Remitir() = nil, want error")
	}
}

func TestRemitirFault(t *testing.T) {
	fault := `<?xml version="1.0"?><soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><soapenv:Fault/></soapenv:Body></soapenv:Envelope>`
	c, _ := newTestClient(t, http.StatusOK, fault)
	_, err := c.Remitir(context.Background(), "<envelope/>")
	if err == nil {
		t.Fatal("Remitir() = nil, want error para fault SOAP")
	}
}

// TestLoadP12RoundTrip genera un P12 real con openssl y lo carga: cubre el
// camino completo del certificado sin depender de la AEAT.
func TestLoadP12RoundTrip(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl no disponible; se salta la prueba del P12")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "key.pem")
	crt := filepath.Join(dir, "crt.pem")
	p12 := filepath.Join(dir, "cert.p12")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("openssl", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("openssl %v: %v: %s", args, err, out)
		}
	}
	run("req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", key, "-out", crt, "-days", "1",
		"-subj", "/CN=Facturable Test SL")
	run("pkcs12", "-export", "-out", p12, "-inkey", key, "-in", crt,
		"-passout", "pass:secreta", "-name", "facturable")

	cert, err := LoadP12(p12, "secreta")
	if err != nil {
		t.Fatalf("LoadP12() = %v", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "Facturable Test SL" {
		t.Fatalf("CN = %q", leaf.Subject.CommonName)
	}
	if _, err := LoadP12(p12, "mal"); err == nil {
		t.Fatal("LoadP12 con contraseña errónea = nil error")
	}
}
