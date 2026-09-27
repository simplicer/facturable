// First-run onboarding: when the agent starts without an emisor identity
// (no SELLO_NIF and no etc/config.json) the wizard collects it here instead
// of dying at startup. Identity is validated against the pure domain NIF,
// applied to the running Service and persisted to etc/config.json so the
// next start boots ready to stamp.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"facturable/internal/sello"
	"facturable/internal/verifactu/domain"
)

// onboardingPayload is the wizard request. cert_p12_path / cert_pass are
// accepted for the wizard flow but not persisted here: certificate install
// keeps its own encrypted path (PUT /v1/certificates → var/certificados).
// TelemetryOptIn is a pointer so an absent field (older wizard clients)
// preserves the stored preference instead of silently opting the user out.
type onboardingPayload struct {
	NIF            string `json:"nif"`
	Nombre         string `json:"nombre"`
	CertP12Path    string `json:"cert_p12_path,omitempty"`
	CertPass       string `json:"cert_pass,omitempty"`
	TelemetryOptIn *bool  `json:"telemetry_opt_in,omitempty"`
}

// emisorConfig is the persisted onboarding result (etc/config.json).
// TelemetryOptIn is the opt-in anonymous telemetry preference; the zero
// value (absent in pre-telemetry configs) means opted out.
type emisorConfig struct {
	EmisorNIF      string `json:"emisor_nif"`
	EmisorNombre   string `json:"emisor_nombre"`
	TelemetryOptIn bool   `json:"telemetry_opt_in,omitempty"`
}

// loadEmisorConfig reads the persisted identity; a missing or corrupted file
// is simply a first run (blank identity), never a startup failure.
func loadEmisorConfig(path string) emisorConfig {
	raw, err := os.ReadFile(path)
	if err != nil {
		return emisorConfig{}
	}
	var cfg emisorConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return emisorConfig{}
	}
	return cfg
}

// saveEmisorConfig persists the identity for the next start (0600: it is
// fiscal identity data), creating the directory when needed.
func saveEmisorConfig(path string, cfg emisorConfig) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// onboardEmisor answers PUT /v1/onboarding: it validates the NIF with the
// pure domain rules, persists the identity and configures the running
// service. nif and nombre are main()'s captured locals, so every other
// endpoint (status, stamps, QR, ledger) sees the new identity immediately
// without a restart.
func onboardEmisor(srv *sello.Service, configPath string, nif, nombre *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req onboardingPayload
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			writeErr(w, fmt.Errorf("no se pudo leer el cuerpo: %v", err))
			return
		}
		valido, err := domain.NewNIF(req.NIF)
		if err != nil {
			// semantic validation of the identity is a 422, not a 400
			writeProblem(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if req.Nombre == "" {
			req.Nombre = valido.Value()
		}
		// The wizard checkbox drives telemetry_opt_in; when the field is
		// absent the stored preference survives (re-onboarding must not
		// silently flip the user's privacy choice).
		prev := loadEmisorConfig(configPath)
		cfg := emisorConfig{EmisorNIF: valido.Value(), EmisorNombre: req.Nombre, TelemetryOptIn: prev.TelemetryOptIn}
		if req.TelemetryOptIn != nil {
			cfg.TelemetryOptIn = *req.TelemetryOptIn
		}
		// Persist first: a failed write must not leave a half-onboarded agent.
		if err := saveEmisorConfig(configPath, cfg); err != nil {
			writeErr(w, fmt.Errorf("no se pudo guardar la configuración: %v", err))
			return
		}
		*nif, *nombre = cfg.EmisorNIF, cfg.EmisorNombre
		srv.EmisorNIF, srv.EmisorNombre = *nif, *nombre
		srv.Sistema.NumeroInstalacion = installationNumber(*nif)
		writeJSON(w, map[string]any{"data": map[string]any{"ok": true, "onboarding": false}})
	}
}
