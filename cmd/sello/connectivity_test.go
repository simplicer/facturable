package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sslmatep12 "software.sslmate.com/src/go-pkcs12"
)

// writeTestP12 generates a real P12/PFX whose leaf expires at notAfter and
// writes it to dir with the password "secreta"; it returns the path. The
// LegacyRC2 container pairs with LoadP12's Decode.
func writeTestP12(t *testing.T, dir string, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Facturable Test SL"},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sslmatep12.LegacyRC2.Encode(key, leaf, nil, "secreta")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cert.p12")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCertExpiryWarning(t *testing.T) {
	now := time.Date(2027, 3, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		notAfter  time.Time
		wantAviso string
	}{
		{
			name:      "ten days left asks for renewal at the FNMT",
			notAfter:  now.Add(10 * 24 * time.Hour),
			wantAviso: "⚠ Tu certificado caduca en 10 días. Renuévalo en la FNMT.",
		},
		{
			name:      "29 days still warns (threshold is under 30)",
			notAfter:  now.Add(29 * 24 * time.Hour),
			wantAviso: "⚠ Tu certificado caduca en 29 días. Renuévalo en la FNMT.",
		},
		{
			name:      "exactly 30 days does not warn",
			notAfter:  now.Add(30 * 24 * time.Hour),
			wantAviso: "",
		},
		{
			name:      "three months away does not warn",
			notAfter:  now.Add(90 * 24 * time.Hour),
			wantAviso: "",
		},
		{
			name:      "already expired asks for renewal too",
			notAfter:  now.Add(-24 * time.Hour),
			wantAviso: "⚠ Tu certificado ha caducado. Renuévalo en la FNMT.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certExpiryWarning(tt.notAfter, now)
			if got != tt.wantAviso {
				t.Errorf("certExpiryWarning() = %q, want %q", got, tt.wantAviso)
			}
		})
	}
}

func TestCertificateState(t *testing.T) {
	dir := t.TempDir()
	t.Run("valid certificate expiring soon reports ok plus the renewal warning", func(t *testing.T) {
		p12 := writeTestP12(t, dir, time.Now().Add(10*24*time.Hour))
		estado, aviso, err := certificateState(p12, "secreta")
		if err != nil {
			t.Fatalf("certificateState() = %v", err)
		}
		if estado != "ok" {
			t.Errorf("estado = %q, want %q", estado, "ok")
		}
		if !strings.Contains(aviso, "Renuévalo en la FNMT") {
			t.Errorf("aviso = %q, want the FNMT renewal warning", aviso)
		}
	})
	t.Run("valid certificate far from expiry reports ok without warning", func(t *testing.T) {
		p12 := writeTestP12(t, dir, time.Now().Add(365*24*time.Hour))
		estado, aviso, err := certificateState(p12, "secreta")
		if err != nil {
			t.Fatalf("certificateState() = %v", err)
		}
		if estado != "ok" || aviso != "" {
			t.Errorf("certificateState() = (%q, %q), want (ok, \"\")", estado, aviso)
		}
	})
	t.Run("wrong password fails", func(t *testing.T) {
		p12 := writeTestP12(t, dir, time.Now().Add(365*24*time.Hour))
		if _, _, err := certificateState(p12, "mal"); err == nil {
			t.Error("certificateState() with wrong password = nil error")
		}
	})
}

func TestCheckConnectivity(t *testing.T) {
	origHandshake := aeatHandshake
	t.Cleanup(func() { aeatHandshake = origHandshake })

	t.Run("without certificate reports simulated mode and the AEAT handshake", func(t *testing.T) {
		t.Setenv("SELLO_CERT_P12", "")
		aeatHandshake = func() error { return nil }
		rec := httptest.NewRecorder()
		checkConnectivity(rec, httptest.NewRequest(http.MethodGet, "/v1/connectivity", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("response is not enveloped JSON: %v", err)
		}
		if out.Data["certificado"] != "no configurado (modo simulado)" {
			t.Errorf("certificado = %v, want %q", out.Data["certificado"], "no configurado (modo simulado)")
		}
		if out.Data["aeat_pruebas"] != "ok" {
			t.Errorf("aeat_pruebas = %v, want %q", out.Data["aeat_pruebas"], "ok")
		}
		if _, has := out.Data["aviso"]; has {
			t.Errorf("aviso = %v, want no warning without a certificate", out.Data["aviso"])
		}
	})

	t.Run("configured certificate expiring soon adds the renewal warning", func(t *testing.T) {
		p12 := writeTestP12(t, t.TempDir(), time.Now().Add(10*24*time.Hour))
		t.Setenv("SELLO_CERT_P12", p12)
		t.Setenv("SELLO_CERT_PASS", "secreta")
		aeatHandshake = func() error { return errors.New("dial boom") }
		rec := httptest.NewRecorder()
		checkConnectivity(rec, httptest.NewRequest(http.MethodGet, "/v1/connectivity", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("response is not enveloped JSON: %v", err)
		}
		if out.Data["certificado"] != "ok" {
			t.Errorf("certificado = %v, want %q", out.Data["certificado"], "ok")
		}
		aviso, _ := out.Data["aviso"].(string)
		if !strings.Contains(aviso, "Renuévalo en la FNMT") {
			t.Errorf("aviso = %q, want the FNMT renewal warning", aviso)
		}
		if out.Data["aeat_pruebas"] != "error: dial boom" {
			t.Errorf("aeat_pruebas = %v, want %q", out.Data["aeat_pruebas"], "error: dial boom")
		}
	})
}
