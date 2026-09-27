package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"facturable/internal/sello"
)

func boolPtr(v bool) *bool { return &v }

// POST /v1/telemetry/optin persists the preference to etc/config.json and
// flips the in-memory flag the daily ticker reads — without a restart.
func TestTelemetryOptInPersistsPreference(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantOptIn  bool
	}{
		{name: "opting in persists true", body: `{"enabled":true}`, wantStatus: http.StatusOK, wantOptIn: true},
		{name: "opting out persists false", body: `{"enabled":false}`, wantStatus: http.StatusOK, wantOptIn: false},
		{name: "malformed body is rejected", body: `{no json`, wantStatus: http.StatusBadRequest, wantOptIn: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "etc", "config.json")
			var optIn atomic.Bool
			handler := telemetryOptIn(configPath, &optIn)

			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodPost, "/v1/telemetry/optin", bytes.NewBufferString(tt.body)))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if got := optIn.Load(); got != tt.wantOptIn {
				t.Errorf("in-memory opt-in = %v, want %v", got, tt.wantOptIn)
			}
			cfg := loadEmisorConfig(configPath)
			if cfg.TelemetryOptIn != tt.wantOptIn {
				t.Errorf("persisted telemetry_opt_in = %v, want %v", cfg.TelemetryOptIn, tt.wantOptIn)
			}
		})
	}
}

// The preference must survive later config writes: opting in must not wipe
// the onboarded identity, and onboarding again must not wipe the opt-in.
func TestTelemetryOptInPreservesOtherConfigFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "etc", "config.json")
	if err := saveEmisorConfig(configPath, emisorConfig{EmisorNIF: "12345678Z", EmisorNombre: "Antonio"}); err != nil {
		t.Fatal(err)
	}

	var optIn atomic.Bool
	optInHandler := telemetryOptIn(configPath, &optIn)
	rec := httptest.NewRecorder()
	optInHandler(rec, httptest.NewRequest(http.MethodPost, "/v1/telemetry/optin", bytes.NewBufferString(`{"enabled":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("optin status = %d", rec.Code)
	}

	cfg := loadEmisorConfig(configPath)
	if cfg.EmisorNIF != "12345678Z" || cfg.EmisorNombre != "Antonio" {
		t.Errorf("opt-in wiped the identity: %+v", cfg)
	}
	if !cfg.TelemetryOptIn || !optIn.Load() {
		t.Errorf("opt-in not recorded: config=%v memory=%v", cfg.TelemetryOptIn, optIn.Load())
	}

	// onboarding again without touching telemetry keeps the stored opt-in
	onboard := onboardEmisor(&sello.Service{}, configPath, new(string), new(string))
	body, _ := json.Marshal(onboardingPayload{NIF: "12345678Z", Nombre: "Antonio García"})
	rec2 := httptest.NewRecorder()
	onboard(rec2, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader(body)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("onboarding status = %d (body: %s)", rec2.Code, rec2.Body.String())
	}
	cfg = loadEmisorConfig(configPath)
	if !cfg.TelemetryOptIn {
		t.Error("re-onboarding wiped telemetry_opt_in")
	}
	if cfg.EmisorNombre != "Antonio García" {
		t.Errorf("onboarding lost the new name: %+v", cfg)
	}
}

// The wizard checkbox (telemetry_opt_in) flows through PUT /v1/onboarding.
func TestOnboardEmisorPersistsTelemetryOptIn(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	handler := onboardEmisor(&sello.Service{}, configPath, new(string), new(string))

	body, _ := json.Marshal(onboardingPayload{NIF: "12345678Z", Nombre: "Antonio", TelemetryOptIn: boolPtr(true)})
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if cfg := loadEmisorConfig(configPath); !cfg.TelemetryOptIn {
		t.Errorf("telemetry_opt_in = %v, want true", cfg.TelemetryOptIn)
	}

	// unchecked (default) must persist as false
	configPath2 := filepath.Join(t.TempDir(), "config.json")
	body2, _ := json.Marshal(onboardingPayload{NIF: "12345678Z", Nombre: "Antonio"})
	rec3 := httptest.NewRecorder()
	onboardEmisor(&sello.Service{}, configPath2, new(string), new(string))(
		rec3, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader(body2)))
	if rec3.Code != http.StatusOK {
		t.Fatalf("status = %d", rec3.Code)
	}
	if cfg := loadEmisorConfig(configPath2); cfg.TelemetryOptIn {
		t.Error("telemetry_opt_in = true, want false (default unchecked)")
	}
}

// GET /v1/telemetry/status answers the current preference for the UI.
func TestTelemetryStatusAnswersOptIn(t *testing.T) {
	var optIn atomic.Bool
	rec := httptest.NewRecorder()
	telemetryStatus(&optIn)(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry/status", nil))

	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not enveloped JSON: %v", err)
	}
	if out.Data["optin"] != false {
		t.Errorf("optin = %v, want false", out.Data["optin"])
	}

	optIn.Store(true)
	rec2 := httptest.NewRecorder()
	telemetryStatus(&optIn)(rec2, httptest.NewRequest(http.MethodGet, "/v1/telemetry/status", nil))
	out.Data = nil
	if err := json.Unmarshal(rec2.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not enveloped JSON: %v", err)
	}
	if out.Data["optin"] != true {
		t.Errorf("optin = %v, want true", out.Data["optin"])
	}
}

// Loading a config written before telemetry existed must default to opt-out.
func TestLoadEmisorConfigDefaultsTelemetryOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"emisor_nif":"12345678Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := loadEmisorConfig(path); cfg.TelemetryOptIn {
		t.Error("pre-telemetry config loaded as opted-in")
	}
}
