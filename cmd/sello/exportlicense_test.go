package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"facturable/internal/licenses"
	"facturable/internal/sello"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// newExportService builds a service over a temporary ledger seeded with one
// record, so the CSV export can be exercised end to end.
func newExportService(t *testing.T) *sello.Service {
	t.Helper()
	store, err := persistence.Abrir(t.TempDir() + "/export.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := &sello.Service{
		Store: store, EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SystemInfo{
			NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
			NombreSistema: "Facturable Sello", IDSistema: "FS", Version: "0.1.0",
			NumeroInstalacion: "00001",
		},
	}
	if _, err := store.Append(persistence.StoredRecord{
		NIF: srv.EmisorNIF, Serie: "F-2027-0001", Date: "05-01-2027", Tipo: "F1",
		Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
		Huella: strings.Repeat("H", 64), XML: "<xml/>",
	}); err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestLicenseStatus pins the X-License-Status classification used by the
// export header (audit abuse #5): "none" without LICENSE_TOKEN (development
// mode), "active" when the gate accepts, "expired" when a token is
// configured but the license no longer grants.
func TestLicenseStatus(t *testing.T) {
	open := func() (bool, string) { return true, "" }
	if got := licenseStatus(open, false); got != "none" {
		t.Fatalf("no token = %q, want none", got)
	}
	if got := licenseStatus(open, true); got != "active" {
		t.Fatalf("valid license = %q, want active", got)
	}
	rejected := func() (bool, string) { return false, licenses.ErrLicenseExpired.Error() }
	if got := licenseStatus(rejected, true); got != "expired" {
		t.Fatalf("expired license = %q, want expired", got)
	}
}

// TestRecordsCSVExportAlwaysAvailableWithLicenseStatus (audit abuse #5): the
// CSV export NEVER blocks on the license (never-hostage), whatever its
// state, and always answers with the informative X-License-Status header.
func TestRecordsCSVExportAlwaysAvailableWithLicenseStatus(t *testing.T) {
	cases := []struct {
		name       string
		token      bool
		gate       func() (bool, string)
		wantStatus string
	}{
		{name: "no token configured", token: false, wantStatus: "none"},
		{name: "active license", token: true, gate: func() (bool, string) { return true, "" }, wantStatus: "active"},
		{name: "expired license", token: true, gate: func() (bool, string) { return false, "la licencia ha caducado" }, wantStatus: "expired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newExportService(t)
			licenciaEstado := func() string { return licenseStatus(tc.gate, tc.token) }
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/records", recordsCollection(srv, srv.Store, srv.EmisorNIF, licenciaEstado))

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/records?format=csv&from=01-01-2027&to=31-12-2027", nil))

			// never-hostage: the export succeeds WITH or WITHOUT a license
			if rec.Code != http.StatusOK {
				t.Fatalf("export status = %d (body: %s), want 200 always", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
				t.Fatalf("content-type = %q, want text/csv", ct)
			}
			if got := rec.Header().Get("X-License-Status"); got != tc.wantStatus {
				t.Fatalf("X-License-Status = %q, want %q", got, tc.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), "F-2027-0001") {
				t.Fatalf("export lost the ledger row: %s", rec.Body.String())
			}
		})
	}
}
