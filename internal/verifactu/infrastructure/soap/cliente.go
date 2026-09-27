// Package soap implements the AEAT Submitter port over SOAP 1.2 with a
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

// AeatEndpoints selects the official endpoint set (verified against the
// production WSDL, see docs/informes/INFORME-VERIFACTU.md §7).
type AeatEndpoints struct {
	PersonURL string
	SealURL   string
}

var (
	// Production son las URLs reales (www10 atiende certificados de sello).
	Production = AeatEndpoints{
		PersonURL: "https://www1.agenciatributaria.gob.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
		SealURL:   "https://www10.agenciatributaria.gob.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
	}
	// Testing is the AEAT pre-production environment.
	Testing = AeatEndpoints{
		PersonURL: "https://prewww1.aeat.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
		SealURL:   "https://prewww10.aeat.es/wlpl/TIKE-CONT/ws/SistemaFacturacion/VerifactuSOAP",
	}
)

// Client es un Submitter SOAP hacia la AEAT.
type Client struct {
	url    string
	http   *http.Client
	certCN string
}

// NewClient builds the client for an environment URL. The certificate can
// be an individual/legal-entity one (www1) or an electronic seal (www10).
func NewClient(url string, cert tls.Certificate) *Client {
	// IMPORTANT: the client certificate goes in Certificates (mTLS); the
	// trust anchors stay the SYSTEM pool (nil RootCAs). Pinning the server
	// leaf into RootCAs here made every real submission fail with
	// "x509: unknown authority" (external audit finding, fixed).
	return &Client{
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

// LoadP12 loads a qualified certificate in P12/PFX format.
// Only the modern format is supported (standard p12 AEAD/SHA1 algorithms).
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

// ErrSOAPFailure marca respuestas HTTP != 200 o faults SOAP.
var ErrSOAPFailure = errors.New("la AEAT rechazó la llamada SOAP")

// Submit implements application.Submitter: it sends the batch signed with
// the client TLS certificate and parses the answer.
func (c *Client) Submit(ctx context.Context, soapXML string) (application.RespuestaLote, error) {
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
			ErrSOAPFailure, resp.StatusCode, excerpt(body, 300))
	}
	lote, err := application.ParseRespuesta(string(body))
	if err != nil {
		// Puede ser un Fault SOAP: se reporta con el detalle disponible.
		return application.RespuestaLote{}, fmt.Errorf("%w: %v: %s", ErrSOAPFailure, err, excerpt(body, 300))
	}
	return lote, nil
}

func excerpt(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
