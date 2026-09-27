// Opt-in anonymous telemetry endpoints. The privacy bar: a ping carries
// only version, os and the invoice count of the last 30 days — never NIFs,
// names or invoice content. Off by default; the preference lives in
// etc/config.json and the daily ticker reads the in-memory flag, so a
// change applies without restarting the agent.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
)

// telemetryOptIn answers POST /v1/telemetry/optin {enabled: bool}: it
// persists the preference to etc/config.json (keeping the rest of the
// config intact) and flips the shared flag the daily ping reads.
func telemetryOptIn(configPath string, optIn *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&req); err != nil {
			writeErr(w, fmt.Errorf("no se pudo leer el cuerpo: %v", err))
			return
		}
		cfg := loadEmisorConfig(configPath)
		cfg.TelemetryOptIn = req.Enabled
		if err := saveEmisorConfig(configPath, cfg); err != nil {
			writeErr(w, fmt.Errorf("no se pudo guardar la preferencia: %v", err))
			return
		}
		optIn.Store(req.Enabled)
		writeJSON(w, map[string]any{"data": map[string]any{"optin": req.Enabled}})
	}
}

// telemetryStatus answers GET /v1/telemetry/status {optin: bool} so the UI
// can render the current state of the footer toggle.
func telemetryStatus(optIn *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": map[string]any{"optin": optIn.Load()}})
	}
}
