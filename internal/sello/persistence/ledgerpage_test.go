package persistence

import (
	"strings"
	"testing"
)

// TestLedgerPageFiltersByEstado: the ledger page query must filter by the
// latest submission state so the UI can answer "show me only the pending
// ones". Records without any submission count as pendiente; the total in
// the pagination meta must respect the filter too.
func TestLedgerPageFiltersByEstado(t *testing.T) {
	s := newTestStore(t)
	idCorrecto, err := s.Append(chainRow(1, strings.Repeat("A", 64), ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, strings.Repeat("B", 64), strings.Repeat("A", 64))); err != nil {
		t.Fatal(err)
	}
	idRechazado, err := s.Append(chainRow(3, strings.Repeat("C", 64), strings.Repeat("B", 64)))
	if err != nil {
		t.Fatal(err)
	}
	idAvisos, err := s.Append(chainRow(4, strings.Repeat("D", 64), strings.Repeat("C", 64)))
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []StoredSubmission{
		{RegistroID: idCorrecto, Estado: "correcto", CSV: "CSV0000000000A"},
		{RegistroID: idRechazado, Estado: "rechazado", Codigo: "0300", Detalle: "NIF del destinatario incorrecto"},
		{RegistroID: idAvisos, Estado: "aceptado_errores"},
	} {
		if err := s.RecordSubmission(sub); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("pendiente keeps only records never sent", func(t *testing.T) {
		rows, total, err := s.LedgerPageByEstado("B72877814", 0, 1_000_000_000, 50, 0, "pendiente")
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 || rows[0].Serie != "F-2027-0002" {
			t.Fatalf("pendiente: rows=%d total=%d serie=%q, want 1 row F-2027-0002", len(rows), total, rows[0].Serie)
		}
		if rows[0].Estado != "pendiente" {
			t.Fatalf("estado = %q, want pendiente", rows[0].Estado)
		}
	})
	t.Run("correcto keeps only the accepted record", func(t *testing.T) {
		rows, total, err := s.LedgerPageByEstado("B72877814", 0, 1_000_000_000, 50, 0, "correcto")
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 || rows[0].Serie != "F-2027-0001" {
			t.Fatalf("correcto: rows=%d total=%d, want 1 row F-2027-0001", len(rows), total)
		}
	})
	t.Run("rechazado keeps only the rejected record", func(t *testing.T) {
		rows, total, err := s.LedgerPageByEstado("B72877814", 0, 1_000_000_000, 50, 0, "rechazado")
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 || rows[0].Serie != "F-2027-0003" {
			t.Fatalf("rechazado: rows=%d total=%d, want 1 row F-2027-0003", len(rows), total)
		}
	})
	t.Run("aceptado_errores keeps only the record accepted with warnings", func(t *testing.T) {
		rows, total, err := s.LedgerPageByEstado("B72877814", 0, 1_000_000_000, 50, 0, "aceptado_errores")
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 || rows[0].Serie != "F-2027-0004" {
			t.Fatalf("aceptado_errores: rows=%d total=%d, want 1 row F-2027-0004", len(rows), total)
		}
	})
	t.Run("empty estado keeps the whole ledger (regression)", func(t *testing.T) {
		rows, total, err := s.LedgerPageByEstado("B72877814", 0, 1_000_000_000, 50, 0, "")
		if err != nil {
			t.Fatal(err)
		}
		if total != 4 || len(rows) != 4 {
			t.Fatalf("sin filtro: rows=%d total=%d, want 4", len(rows), total)
		}
	})
}
