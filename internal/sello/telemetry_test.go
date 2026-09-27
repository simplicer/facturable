package sello

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Telemetry is opt-in and anonymous by contract: the ping body must carry
// version, os and the invoice count and NOTHING else (no NIF, no names, no
// invoice content). These tests pin that contract.
func TestTelemetryPingSendsAnonymousCounters(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tel := NewTelemetry(srv.URL)
	if err := tel.Ping(context.Background(), "1.2.3", "linux", 42); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotPath != "/v1/telemetry" {
		t.Errorf("ping path = %q, want /v1/telemetry", gotPath)
	}
	if gotBody["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", gotBody["version"])
	}
	if gotBody["os"] != "linux" {
		t.Errorf("os = %v, want linux", gotBody["os"])
	}
	if gotBody["invoices_last_30d"] != float64(42) {
		t.Errorf("invoices_last_30d = %v, want 42", gotBody["invoices_last_30d"])
	}
	if len(gotBody) != 3 {
		t.Errorf("ping carries %d fields (%v), want exactly 3 anonymous ones", len(gotBody), gotBody)
	}
}

func TestTelemetryPingReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tel := NewTelemetry(srv.URL)
	if err := tel.Ping(context.Background(), "1.2.3", "linux", 0); err == nil {
		t.Fatal("Ping against a failing server should report an error")
	}
}

func TestTelemetryPingReportsConnectionFailure(t *testing.T) {
	tel := NewTelemetry("http://127.0.0.1:1") // nothing listens there
	if err := tel.Ping(context.Background(), "1.2.3", "linux", 0); err == nil {
		t.Fatal("Ping against a dead endpoint should report an error")
	}
}
