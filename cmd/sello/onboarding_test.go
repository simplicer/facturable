package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"facturable/internal/sello"
)

func TestOnboardEmisor(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]string
		wantStatus int
		wantNIF    string
		wantName   string
		wantConfig bool
	}{
		{
			name:       "valid nif configures the service and persists the identity",
			body:       map[string]string{"nif": " 12345678z ", "nombre": "Antonio García"},
			wantStatus: http.StatusOK,
			wantNIF:    "12345678Z",
			wantName:   "Antonio García",
			wantConfig: true,
		},
		{
			name:       "empty nombre falls back to the nif",
			body:       map[string]string{"nif": "12345678Z"},
			wantStatus: http.StatusOK,
			wantNIF:    "12345678Z",
			wantName:   "12345678Z",
			wantConfig: true,
		},
		{
			name:       "invalid control letter rejects the whole onboarding",
			body:       map[string]string{"nif": "12345678A", "nombre": "Impostor"},
			wantStatus: http.StatusBadRequest,
			wantConfig: false,
		},
		{
			name:       "empty nif is rejected",
			body:       map[string]string{"nombre": "Alguien"},
			wantStatus: http.StatusBadRequest,
			wantConfig: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &sello.Service{}
			configPath := filepath.Join(t.TempDir(), "etc", "config.json")
			nif, nombre := "", ""
			handler := onboardEmisor(srv, configPath, &nif, &nombre)

			raw, _ := json.Marshal(tt.body)
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader(raw)))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if srv.EmisorNIF != tt.wantNIF {
				t.Errorf("srv.EmisorNIF = %q, want %q", srv.EmisorNIF, tt.wantNIF)
			}
			if srv.EmisorNombre != tt.wantName {
				t.Errorf("srv.EmisorNombre = %q, want %q", srv.EmisorNombre, tt.wantName)
			}
			if nif != tt.wantNIF || nombre != tt.wantName {
				t.Errorf("captured locals = (%q, %q), want (%q, %q)", nif, nombre, tt.wantNIF, tt.wantName)
			}
			_, err := os.Stat(configPath)
			if tt.wantConfig && err != nil {
				t.Errorf("config not persisted: %v", err)
			}
			if !tt.wantConfig && err == nil {
				t.Error("config persisted for a rejected onboarding")
			}
		})
	}
}

func TestOnboardEmisorAnswersOnboardingFalse(t *testing.T) {
	srv := &sello.Service{}
	handler := onboardEmisor(srv, filepath.Join(t.TempDir(), "config.json"), new(string), new(string))

	raw, _ := json.Marshal(map[string]string{"nif": "12345678Z", "nombre": "Antonio"})
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader(raw)))

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if out["ok"] != true || out["onboarding"] != false {
		t.Errorf("response = %v, want {ok:true, onboarding:false}", out)
	}
}

func TestOnboardEmisorMalformedBody(t *testing.T) {
	srv := &sello.Service{}
	configPath := filepath.Join(t.TempDir(), "config.json")
	handler := onboardEmisor(srv, configPath, new(string), new(string))

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPut, "/v1/onboarding", bytes.NewReader([]byte("{no json"))))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if _, err := os.Stat(configPath); err == nil {
		t.Error("config persisted for a malformed body")
	}
}

func TestLoadEmisorConfig(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantNIF  string
		wantName string
	}{
		{name: "existing file provides the stored identity", content: `{"emisor_nif":"12345678Z","emisor_nombre":"Antonio García"}`, wantNIF: "12345678Z", wantName: "Antonio García"},
		{name: "empty object is a blank identity", content: `{}`},
		{name: "corrupted file behaves as first run", content: `{not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := loadEmisorConfig(path)
			if cfg.EmisorNIF != tt.wantNIF || cfg.EmisorNombre != tt.wantName {
				t.Errorf("loadEmisorConfig() = %+v, want {%q %q}", cfg, tt.wantNIF, tt.wantName)
			}
		})
	}
}

func TestSaveEmisorConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "nested", "config.json")
	want := emisorConfig{EmisorNIF: "12345678Z", EmisorNombre: "Antonio García"}

	if err := saveEmisorConfig(path, want); err != nil {
		t.Fatalf("saveEmisorConfig: %v", err)
	}
	got := loadEmisorConfig(path)
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config permissions = %v, want 0600 (fiscal identity)", info.Mode().Perm())
	}
}
