package sello

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Telemetry publishes the opt-in anonymous ping to the public API. The
// contract is privacy by construction: a ping carries ONLY the agent
// version, the operating system and how many invoices were stamped in the
// last 30 days — never NIFs, names, amounts or any invoice content. The
// invoice count is computed locally by the agent's ledger; nothing else
// ever leaves the machine.
type Telemetry struct {
	APIBase string // e.g. https://api.facturable.es (FACTURABLE_API_URL)
	// HTTP is injectable for tests; zero value means a 30-second timeout.
	HTTP *http.Client
}

// NewTelemetry builds the telemetry client against the public API base.
func NewTelemetry(apiBase string) *Telemetry {
	return &Telemetry{APIBase: apiBase, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (t *Telemetry) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// ping is the exact anonymous payload; adding a field here means adding it
// to the privacy promise first.
type ping struct {
	Version         string `json:"version"`
	OS              string `json:"os"`
	InvoicesLast30d int    `json:"invoices_last_30d"`
}

// Ping sends one anonymous ping. Failures are returned so the caller can log
// them and move on: telemetry must never disturb the fiscal workflow.
func (t *Telemetry) Ping(ctx context.Context, version, os string, invoicesLast30d int) error {
	body, err := json.Marshal(ping{Version: version, OS: os, InvoicesLast30d: invoicesLast30d})
	if err != nil {
		return fmt.Errorf("no se pudo codificar el ping: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(t.APIBase, "/")+"/v1/telemetry", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no se pudo preparar el ping: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client().Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo enviar el ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ping de telemetría: HTTP %d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}
