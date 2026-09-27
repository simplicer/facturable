package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"facturable/internal/sello/persistence"
)

// TestSecurityHeadersOnEveryResponse: the agent answers browsers, so every
// response carries X-Content-Type-Options: nosniff and X-Frame-Options: DENY
// (audit network/input #4).
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
}

// TestWriteErrHidesInternalDetail: persistence failures marked with
// persistence.ErrInternal must reach the client as a generic 500 problem+json
// (audit network/input #8) — the SQL text stays in the server log only.
func TestWriteErrHidesInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, fmt.Errorf("%w: sqlite: no such table: ledger", persistence.ErrInternal))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	body := rec.Body.String()
	if strings.Contains(body, "sqlite") || strings.Contains(body, "no such table") {
		t.Fatalf("internal detail leaked to the client: %s", body)
	}
	if !strings.Contains(body, "error interno") {
		t.Fatalf("missing generic message: %s", body)
	}
}

// TestWriteErrKeepsBusinessDetail: plain validation errors keep their 400 and
// their (Spanish, user-facing) detail.
func TestWriteErrKeepsBusinessDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, errors.New("falta la base imponible total"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "falta la base imponible total") {
		t.Fatalf("business detail lost: %s", rec.Body.String())
	}
}

// TestWriteErrUnwrapsWrappedInternal: the sentinel is detected through
// fmt.Errorf %w chains, not only at the top level.
func TestWriteErrUnwrapsWrappedInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	inner := fmt.Errorf("%w: disk I/O error", persistence.ErrInternal)
	writeErr(rec, fmt.Errorf("envío de lote: %w", inner))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "disk I/O") {
		t.Fatalf("internal detail leaked: %s", rec.Body.String())
	}
}
