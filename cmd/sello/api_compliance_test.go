package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"facturable/internal/sello"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// newComplianceService builds a service over a temporary ledger so the API
// handlers can be exercised end to end without the AEAT or a certificate.
func newComplianceService(t *testing.T) *sello.Service {
	t.Helper()
	store, err := persistence.Abrir(t.TempDir() + "/agente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &sello.Service{
		Store: store, EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SystemInfo{
			NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
			NombreSistema: "Facturable Sello", IDSistema: "FS", Version: "0.1.0",
			NumeroInstalacion: "00001",
		},
	}
}

// The security-header middleware must also forbid caching: fiscal documents
// (PDFs, the ledger) must never sit in browser or intermediary caches.
func TestSecurityHeadersIncludeCacheControlNoStore(t *testing.T) {
	rec := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/records", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

// GET /v1/records JSON must be paginated: ?page & ?page-size with defaults
// (1 and 50) and a meta object {page, page_size, total} next to the data
// envelope (Zalando pagination).
func TestRecordsPagePagination(t *testing.T) {
	srv := newComplianceService(t)
	for i := 0; i < 3; i++ {
		row := persistence.StoredRecord{
			NIF: srv.EmisorNIF, Serie: "F-2027-000" + string(rune('1'+i)),
			Date: "05-01-2027", Tipo: "F1", Generado: "2027-01-05T10:00:00+01:00",
			Cuota: "21.00", Importe: "121.00", Huella: strings.Repeat("H", 63) + string(rune('1'+i)),
			XML: "<xml/>",
		}
		if _, err := srv.Store.Append(row); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("defaults answer page 1 with page-size 50 and the total", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
		}
		var out struct {
			Data []json.RawMessage `json:"data"`
			Meta struct {
				Page     int `json:"page"`
				PageSize int `json:"page_size"`
				Total    int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not the enveloped shape: %v", err)
		}
		if len(out.Data) != 3 {
			t.Errorf("data rows = %d, want 3", len(out.Data))
		}
		if out.Meta.Page != 1 || out.Meta.PageSize != 50 || out.Meta.Total != 3 {
			t.Errorf("meta = %+v, want {1 50 3}", out.Meta)
		}
	})

	t.Run("explicit page and page-size slice the collection", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records?page=2&page-size=2", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var out struct {
			Data []json.RawMessage `json:"data"`
			Meta struct {
				Page     int `json:"page"`
				PageSize int `json:"page_size"`
				Total    int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if len(out.Data) != 1 || out.Meta.Page != 2 || out.Meta.PageSize != 2 || out.Meta.Total != 3 {
			t.Fatalf("rows=%d meta=%+v, want 1 row and {2 2 3}", len(out.Data), out.Meta)
		}
	})

	t.Run("invalid pagination values are a 422 validation problem", func(t *testing.T) {
		for _, url := range []string{
			"/v1/records?page=0",
			"/v1/records?page=abc",
			"/v1/records?page-size=0",
			"/v1/records?page-size=501",
		} {
			rec := httptest.NewRecorder()
			writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, url, nil), srv.Store, srv.EmisorNIF)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s: status = %d, want 422 (body: %s)", url, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s: content-type = %q, want problem+json", url, ct)
			}
		}
	})
}

// PUT /v1/invoices/{uuid} is the idempotent creation of the quick invoice
// with a client-provided UUID: 201 on creation, 200 on replay of the same
// invoice, 409 when the UUID was used for a different invoice, 400 for a
// malformed UUID (Zalando).
func TestPutQuickInvoiceIdempotentClientUUID(t *testing.T) {
	srv := newComplianceService(t)
	// the handler reads the UUID with r.PathValue, so it must be exercised
	// through a real ServeMux route match
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/invoices/{uuid}", putQuickInvoice(srv))
	body := strings.NewReader(`{"cliente":"ACME SL","nif":"B44531218","concepto":"Servicios enero","base":"1000.00"}`)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/invoices/7e6a3140-9e5a-4d35-9a5d-3b21f8d1a111", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("creation status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Data struct {
			Sellada bool   `json:"sellada"`
			UUID    string `json:"uuid"`
			Serie   string `json:"serie"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("creation body is not enveloped JSON: %v", err)
	}
	if !out.Data.Sellada || out.Data.UUID != "7e6a3140-9e5a-4d35-9a5d-3b21f8d1a111" || out.Data.Serie == "" {
		t.Fatalf("creation data = %+v", out.Data)
	}
	// the record entered the ledger under the client-provided UUID
	if _, ok, err := srv.Store.ByUUID(out.Data.UUID); err != nil || !ok {
		t.Fatalf("record not stored under the client UUID: ok=%v err=%v", ok, err)
	}

	// replay of the same invoice: 200, no double stamping
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPut, "/v1/invoices/7e6a3140-9e5a-4d35-9a5d-3b21f8d1a111",
		strings.NewReader(`{"cliente":"ACME SL","nif":"B44531218","concepto":"Servicios enero","base":"1000.00"}`)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay status = %d (body: %s)", rec2.Code, rec2.Body.String())
	}

	// same UUID, different invoice: conflict
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, httptest.NewRequest(http.MethodPut, "/v1/invoices/7e6a3140-9e5a-4d35-9a5d-3b21f8d1a111",
		strings.NewReader(`{"cliente":"Otro SL","nif":"B44531218","concepto":"Otra cosa","base":"250.00"}`)))
	if rec3.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409 (body: %s)", rec3.Code, rec3.Body.String())
	}

	// malformed UUID: rejected before any stamping
	rec4 := httptest.NewRecorder()
	mux.ServeHTTP(rec4, httptest.NewRequest(http.MethodPut, "/v1/invoices/not-a-uuid",
		strings.NewReader(`{"cliente":"ACME","concepto":"x","base":"10.00"}`)))
	if rec4.Code != http.StatusBadRequest {
		t.Fatalf("malformed uuid status = %d, want 400", rec4.Code)
	}
}
