// Package soap implements the AEAT Remisor port over SOAP 1.2 with a
// client TLS certificate loaded from a P12/PFX file (qualified certificate
// of the taxpayer or electronic seal).
package soap

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"facturable/internal/verifactu/application"

	sslmatep12 "software.sslmate.com/src/go-pkcs12"
)

// EntornoAEAT selects the official endpoint set (verified against the
// production WSDL, see docs/informes/INFORME-VERIFACTU.md §7).
type EntornoAEAT struct {
	VerifactuPersona string
	VerifactuSello   string
}

var (
	// Produccion son las URLs reales (www10 atiende certificados de sello).
	Produccion = EntornoAEAT{
		VerifactuPersona: "https://www1.agenciatributaria.gob.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
		VerifactuSello:   "https://www10.agenciatributaria.gob.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
	}
	// Pruebas es el entorno de preproducción de la AEAT.
	Pruebas = EntornoAEAT{
		VerifactuPersona: "https://prewww1.aeat.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
		VerifactuSello:   "https://prewww10.aeat.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
	}
)

// Cliente es un Remisor SOAP hacia la AEAT.
type Cliente struct {
	url    string
	http   *http.Client
	certCN string
}

// NewCliente construye el cliente para una URL de entorno. El certificado
// puede ser de persona física/jurídica (www1) o de sello (www10).
func NewCliente(url string, cert tls.Certificate) *Cliente {
	// IMPORTANT: the client certificate goes in Certificates (mTLS); the
	// trust anchors stay the SYSTEM pool (nil RootCAs). Pinning the server
	// leaf into RootCAs here made every real submission fail with
	// "x509: unknown authority" (external audit finding, fixed).
	return &Cliente{
		url: url,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS12,
				},
			},
		},
	}
}

// LoadP12 carga un certificado cualificado en formato P12/PFX.
// Sólo admite formato moderno (AEAD/SHA1 algoritmos estándar de p12).
func LoadP12(path, password string) (tls.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("no se pudo leer el certificado %s: %w", path, err)
	}
	priv, cert, err := sslmatep12.Decode(raw, password)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("el certificado no es un P12 válido o la contraseña no es correcta: %w", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  priv,
	}, nil
}

// ErrFalloSOAP marca respuestas HTTP != 200 o faults SOAP.
var ErrFalloSOAP = errors.New("la AEAT rechazó la llamada SOAP")

// Remitir implementa application.Remisor: envía el lote firmado con el
// certificado TLS de cliente y parsea la respuesta.
func (c *Cliente) Remitir(ctx context.Context, soapXML string) (application.RespuestaLote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader([]byte(soapXML)))
	if err != nil {
		return application.RespuestaLote{}, fmt.Errorf("no se pudo preparar el envío a la AEAT: %w", err)
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return application.RespuestaLote{}, fmt.Errorf("fallo de red contra la AEAT (%s): %w", c.url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return application.RespuestaLote{}, fmt.Errorf("no se pudo leer la respuesta de la AEAT: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return application.RespuestaLote{}, fmt.Errorf("%w: HTTP %d: %s",
			ErrFalloSOAP, resp.StatusCode, excerpt(body, 300))
	}
	lote, err := application.ParseRespuesta(string(body))
	if err != nil {
		// Puede ser un Fault SOAP: se reporta con el detalle disponible.
		return application.RespuestaLote{}, fmt.Errorf("%w: %v: %s", ErrFalloSOAP, err, excerpt(body, 300))
	}
	return lote, nil
}

func excerpt(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
