package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"facturable/internal/sello/persistence"
)

// seedLedgerWithStates appends three records and drives two of them through
// a submission so the collection has one pendiente, one correcto and one
// rechazado row to filter on.
func seedLedgerWithStates(t *testing.T, store *persistence.Store, nif string) {
	t.Helper()
	series := []string{"F-2027-PEND", "F-2027-OK", "F-2027-RECH"}
	ids := make([]int64, 0, len(series))
	for i, serie := range series {
		id, err := store.Append(persistence.StoredRecord{
			NIF: nif, Serie: serie, Date: "05-01-2027", Tipo: "F1",
			Generado: "2027-01-05T10:00:00+0100",
			Cuota:    "21.00", Importe: "121.00",
			Huella: strings.Repeat("H", 63) + string(rune('1'+i)), XML: "<xml/>",
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for _, sub := range []persistence.StoredSubmission{
		{RegistroID: ids[1], Estado: "correcto", CSV: "CSV0000000000A"},
		{RegistroID: ids[2], Estado: "rechazado", Codigo: "0300", Detalle: "NIF del destinatario incorrecto"},
	} {
		if err := store.RecordSubmission(sub); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRecordsEstadoFilter: GET /v1/records must accept ?estado= so the user
// can see only the records in one state (e.g. the pending ones waiting for
// the next submission cycle). An unknown estado is a 422 validation problem
// (Zalando: query params select/filter the representation).
func TestRecordsEstadoFilter(t *testing.T) {
	srv := newComplianceService(t)
	seedLedgerWithStates(t, srv.Store, srv.EmisorNIF)

	t.Run("pendiente answers only the never-sent record", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records?estado=pendiente", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
		}
		var out struct {
			Data []persistence.LedgerRow `json:"data"`
			Meta struct{ Total int `json:"total"` } `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not enveloped JSON: %v", err)
		}
		if out.Meta.Total != 1 || len(out.Data) != 1 || out.Data[0].Serie != "F-2027-PEND" {
			t.Fatalf("pendiente: data=%+v meta.total=%d, want only F-2027-PEND", out.Data, out.Meta.Total)
		}
	})

	t.Run("rechazado answers only the rejected record", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records?estado=rechazado", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
		}
		var out struct {
			Data []persistence.LedgerRow `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not enveloped JSON: %v", err)
		}
		if len(out.Data) != 1 || out.Data[0].Serie != "F-2027-RECH" || out.Data[0].Estado != "rechazado" {
			t.Fatalf("rechazado: data=%+v, want only F-2027-RECH", out.Data)
		}
	})

	t.Run("correcto answers only the submitted record", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records?estado=correcto", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
		}
		var out struct {
			Data []persistence.LedgerRow `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not enveloped JSON: %v", err)
		}
		if len(out.Data) != 1 || out.Data[0].Serie != "F-2027-OK" {
			t.Fatalf("correcto: data=%+v, want only F-2027-OK", out.Data)
		}
	})

	t.Run("aceptado_errores is a valid filter value", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records?estado=aceptado_errores", nil), srv.Store, srv.EmisorNIF)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("no filter answers the whole collection (regression)", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, "/v1/records", nil), srv.Store, srv.EmisorNIF)
		var out struct {
			Data []json.RawMessage    `json:"data"`
			Meta struct{ Total int `json:"total"` } `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not enveloped JSON: %v", err)
		}
		if out.Meta.Total != 3 || len(out.Data) != 3 {
			t.Fatalf("sin filtro: rows=%d total=%d, want 3", len(out.Data), out.Meta.Total)
		}
	})

	t.Run("unknown estado is a 422 validation problem", func(t *testing.T) {
		for _, url := range []string{
			"/v1/records?estado=bogus",
			"/v1/records?estado=PENDIENTE",
			"/v1/records?estado=",
		} {
			rec := httptest.NewRecorder()
			writeRecordsPage(rec, httptest.NewRequest(http.MethodGet, url, nil), srv.Store, srv.EmisorNIF)
			if url == "/v1/records?estado=" {
				// an absent filter is valid: empty value means no filtering
				if rec.Code != http.StatusOK {
					t.Errorf("%s: status = %d, want 200", url, rec.Code)
				}
				continue
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s: status = %d, want 422 (body: %s)", url, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s: content-type = %q, want problem+json", url, ct)
			}
		}
	})
}

// TestWriteExtractProblemCarriesOCRMethod: when neither direct text nor OCR
// is available the extract failure must carry method:"ocr" so the UI can
// tell a scanned-invoice user exactly what to install instead of leaving
// them blocked (audit UX).
func TestWriteExtractProblemCarriesOCRMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	writeExtractProblem(rec, errors.New(`exec: "tesseract": executable file not found in $PATH`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not problem+json: %v", err)
	}
	if out["method"] != "ocr" {
		t.Fatalf("method = %v, want \"ocr\"", out["method"])
	}
	detail, _ := out["detail"].(string)
	if !strings.Contains(detail, "OCR") {
		t.Fatalf("detail lost the OCR mention: %q", detail)
	}
}
