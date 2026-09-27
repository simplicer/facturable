package persistence

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Abrir(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatalf("Abrir(): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func chainRow(n int, huella, enlaceHuella string) StoredRecord {
	return StoredRecord{
		NIF: "B72877814", Serie: "F-2027-000" + string(rune('0'+n)),
		Date: "05-01-2027", Tipo: "F1", Generado: "2027-01-05T10:00:00+01:00",
		Cuota: "21.00", Importe: "121.00", Huella: huella, XML: "<xml/>",
		EnlaceSerie: "F-2027-000" + string(rune('0'+n-1)), EnlaceFecha: "05-01-2027", EnlaceHuella: enlaceHuella,
	}
}

func TestCadenaYAnterior(t *testing.T) {
	s := newTestStore(t)
	if _, _, _, _, _, _, _, err := s.Last("B72877814"); err != nil {
		t.Fatalf("Last en cadena vacía: %v", err)
	}
	if _, _, _, ok, _ := s.Previous("B72877814"); ok {
		t.Fatal("Previous en cadena vacía debería ser !ok")
	}

	h1 := strings.Repeat("A", 64)
	h2 := strings.Repeat("B", 64)
	if _, err := s.Append(chainRow(1, h1, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, h2, h1)); err != nil {
		t.Fatal(err)
	}

	series, fecha, huella, es, ef, eh, ok, err := s.Last("B72877814")
	if err != nil || !ok {
		t.Fatalf("Last: ok=%v err=%v", ok, err)
	}
	if series != "F-2027-0002" || huella != h2 || es != "F-2027-0001" || eh != h1 || ef != "05-01-2027" {
		t.Fatalf("Last = %s %s %s / %s %s %s", series, fecha, huella, es, ef, eh)
	}
	aSerie, _, aHuella, aOk, err := s.Previous("B72877814")
	if err != nil || !aOk || aSerie != "F-2027-0001" || aHuella != h1 {
		t.Fatalf("Previous = %s %s ok=%v err=%v", aSerie, aHuella, aOk, err)
	}
}

func TestRegistrosInmutables(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Append(chainRow(1, strings.Repeat("A", 64), ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE registros SET cuota='0.00' WHERE id=?;`, id); err == nil {
		t.Fatal("UPDATE sobre registros debe abortar (inmutabilidad RRSIF)")
	}
	if _, err := s.db.Exec(`DELETE FROM registros WHERE id=?;`, id); err == nil {
		t.Fatal("DELETE sobre registros debe abortar (inmutabilidad RRSIF)")
	}
}

func TestRemisionesYListado(t *testing.T) {
	s := newTestStore(t)
	id, err := s.Append(chainRow(1, strings.Repeat("C", 64), ""))
	if err != nil {
		t.Fatal(err)
	}

	filas, err := s.Ledger("B72877814", 0, 1_000_000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(filas) != 1 || filas[0].Estado != "pendiente" {
		t.Fatalf("listado inicial = %+v", filas)
	}

	if err := s.RecordSubmission(StoredSubmission{RegistroID: id, Estado: "correcto", CSV: "ABC123DEF456GH78"}); err != nil {
		t.Fatal(err)
	}
	// Un rechazo posterior (2º intento) debe prevalecer como último estado.
	if err := s.RecordSubmission(StoredSubmission{RegistroID: id, Estado: "rechazado", Codigo: "1106", Detalle: "importe"}); err != nil {
		t.Fatal(err)
	}

	filas, err = s.Ledger("B72877814", 0, 1_000_000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(filas) != 1 || filas[0].Estado != "rechazado" || filas[0].CSV != "ABC123DEF456GH78" {
		t.Fatalf("listado tras remisiones = %+v", filas)
	}
}

func TestHuellaUnica(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Append(chainRow(1, strings.Repeat("D", 64), "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, strings.Repeat("D", 64), "")); err == nil {
		t.Fatal("una huella duplicada no debe poder guardarse")
	}
}

// CountRecent feeds the opt-in anonymous telemetry ping (invoices created in
// the last 30 days): only the integer count may ever leave the machine, so
// it must be exact and scoped to the taxpayer.
func TestCountRecent(t *testing.T) {
	s := newTestStore(t)
	if n, err := s.CountRecent("B72877814", time.Now().Add(-30*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("CountRecent on an empty ledger = (%d, %v), want (0, nil)", n, err)
	}

	if _, err := s.Append(chainRow(1, strings.Repeat("A", 64), "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, strings.Repeat("B", 64), strings.Repeat("A", 64))); err != nil {
		t.Fatal(err)
	}
	// a third record backdated 60 days: outside the window. Records are
	// immutable (RRSIF triggers), so this test-only temp store drops the
	// update trigger to backdate one row and restores it right after.
	id3, err := s.Append(chainRow(3, strings.Repeat("C", 64), strings.Repeat("B", 64)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER registros_inmutables_update;`); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE registros SET creado_en=? WHERE id=?;`, old, id3); err != nil {
		t.Fatalf("backdate creado_en: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER registros_inmutables_update
BEFORE UPDATE ON registros
BEGIN SELECT RAISE(ABORT, 'los registros de facturación son inmutables'); END;`); err != nil {
		t.Fatal(err)
	}
	// and one record of another taxpayer inside the window: never counted
	other := chainRow(1, strings.Repeat("E", 64), "")
	other.NIF = "B11111111"
	if _, err := s.Append(other); err != nil {
		t.Fatal(err)
	}

	n, err := s.CountRecent("B72877814", time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("CountRecent: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountRecent = %d, want 2 (the 60-day-old row and the other taxpayer's row stay out)", n)
	}
}

// diskFull classifies driver write failures: SQLITE_FULL and the
// SQLITE_IOERR family must be recognizable so Append can answer the user
// with a clear disk-full message instead of raw driver text.
func TestDiskFullClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error is not disk full", err: nil, want: false},
		{name: "sqlite full maps to disk full", err: errors.New("database or disk is full (13)"), want: true},
		{name: "disk I/O error maps to disk full", err: errors.New("disk I/O error (10)"), want: true},
		{name: "wrapped disk I/O error maps to disk full", err: fmt.Errorf("no se pudo guardar el registro: %w", errors.New("disk I/O error")), want: true},
		{name: "unique violation is not disk full", err: errors.New("UNIQUE constraint failed: registros.huella (2067)"), want: false},
		{name: "busy is not disk full", err: errors.New("database is locked (5) (SQLITE_BUSY)"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diskFull(tt.err); got != tt.want {
				t.Errorf("diskFull(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// A failing disk must produce the clear Spanish message, never driver text.
func TestAppendDiskFullMessage(t *testing.T) {
	err := saveError(errors.New("disk I/O error (10)"))
	if err == nil || err.Error() != "el disco está lleno, no se puede guardar la factura" {
		t.Fatalf("saveError(disk I/O error) = %v, want the clear disk-full message", err)
	}
	other := saveError(errors.New("UNIQUE constraint failed: registros.huella (2067)"))
	if other == nil || strings.Contains(other.Error(), "UNIQUE") == false {
		t.Fatalf("saveError(other) = %v, want the wrapped detail preserved", other)
	}
}

// Append must checkpoint the WAL automatically every 100 records so the
// write-ahead log never grows unbounded on long-lived agents.
func TestAppendAutoCheckpointEvery100Records(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.sqlite"
	s, err := Abrir(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.Append(chainRow(1, strings.Repeat("A", 64), "")); err != nil {
		t.Fatal(err)
	}
	// after the first append the WAL holds data (no checkpoint yet)
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() == 0 {
		t.Fatal("WAL is empty before reaching the checkpoint threshold: the test cannot observe the auto-checkpoint")
	}
	for i := 2; i <= 100; i++ {
		row := chainRow(i%10, strings.Repeat("H", 60)+fmt.Sprintf("%04d", i), strings.Repeat("B", 64))
		if _, err := s.Append(row); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	// crossing the 100-insert threshold must have truncated the WAL
	fi, err := os.Stat(path + "-wal")
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return // no WAL file means nothing left to flush: checkpointed
	}
	if fi.Size() != 0 {
		t.Fatalf("WAL size after 100 inserts = %d bytes, want 0 (TRUNCATE checkpoint)", fi.Size())
	}
}

// LedgerPage returns one page of rows plus the total in the range, feeding
// the {page, page_size, total} meta of GET /v1/records.
func TestLedgerPage(t *testing.T) {
	s := newTestStore(t)
	const total = 5
	for i := 1; i <= total; i++ {
		if _, err := s.Append(chainRow(i, strings.Repeat(string(rune('A'+i)), 64), "")); err != nil {
			t.Fatal(err)
		}
	}
	rows, n, err := s.LedgerPage("B72877814", 0, 1_000_000, 2, 0)
	if err != nil || n != total {
		t.Fatalf("page 1: rows=%d total=%d err=%v, want total=%d err=nil", len(rows), n, err, total)
	}
	if len(rows) != 2 || rows[0].Serie != "F-2027-0001" || rows[1].Serie != "F-2027-0002" {
		t.Fatalf("page 1 rows = %+v", rows)
	}
	rows, n, err = s.LedgerPage("B72877814", 0, 1_000_000, 2, 4)
	if err != nil || n != total {
		t.Fatalf("last page: total=%d err=%v", n, err)
	}
	if len(rows) != 1 || rows[0].Serie != "F-2027-0005" {
		t.Fatalf("last page rows = %+v", rows)
	}
	// offset beyond the end: empty page, total intact
	rows, n, err = s.LedgerPage("B72877814", 0, 1_000_000, 2, 40)
	if err != nil || n != total || len(rows) != 0 {
		t.Fatalf("empty page: rows=%d total=%d err=%v", len(rows), n, err)
	}
}

// TestCountRecords: the total number of ledger records of a taxpayer. The
// scheduled backup uses it to decide whether there is anything to protect;
// an empty NIF counts every taxpayer (same convention as Ledger).
func TestCountRecords(t *testing.T) {
	s := newTestStore(t)
	n, err := s.CountRecords("B72877814")
	if err != nil || n != 0 {
		t.Fatalf("CountRecords en libro vacío = %d, %v; want 0", n, err)
	}
	if _, err := s.Append(chainRow(1, strings.Repeat("A", 64), "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, strings.Repeat("B", 64), strings.Repeat("A", 64))); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountRecords("B72877814"); err != nil || n != 2 {
		t.Fatalf("CountRecords = %d, %v; want 2", n, err)
	}
	if n, err := s.CountRecords("OTRO-NIF"); err != nil || n != 0 {
		t.Fatalf("CountRecords de otro NIF = %d, %v; want 0", n, err)
	}
	if n, err := s.CountRecords(""); err != nil || n != 2 {
		t.Fatalf("CountRecords sin NIF = %d, %v; want 2 (todos los contribuyentes)", n, err)
	}
}

// TestLedgerDevuelveCliente: the ledger view carries the recipient NIF and
// name (the tax-advisor export needs them for model 303). Rows stamped
// before client memory existed come back empty, never an error.
func TestLedgerDevuelveCliente(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Append(StoredRecord{
		NIF: "B72877814", Serie: "F-2027-0001", Date: "05-01-2027", Tipo: "F1",
		Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
		Huella: strings.Repeat("A", 64), XML: "<xml/>",
		ClientNIF: "B12345678",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(chainRow(2, strings.Repeat("B", 64), strings.Repeat("A", 64))); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertClient("B12345678", "Cliente SL"); err != nil {
		t.Fatal(err)
	}
	filas, err := s.Ledger("B72877814", 0, 1_000_000, 10)
	if err != nil || len(filas) != 2 {
		t.Fatalf("Ledger = %d filas, %v", len(filas), err)
	}
	// oldest first: the second row was stamped without client memory
	if filas[1].ClientNIF != "" || filas[1].ClientName != "" {
		t.Fatalf("fila antigua con cliente = %q/%q, want vacío", filas[1].ClientNIF, filas[1].ClientName)
	}
	if filas[0].ClientNIF != "B12345678" || filas[0].ClientName != "Cliente SL" {
		t.Fatalf("cliente = %q/%q, want B12345678/Cliente SL", filas[0].ClientNIF, filas[0].ClientName)
	}
	pag, total, err := s.LedgerPage("B72877814", 0, 1_000_000, 10, 0)
	if err != nil || total != 2 || len(pag) != 2 {
		t.Fatalf("LedgerPage = %d filas, total %d, %v", len(pag), total, err)
	}
	if pag[0].ClientNIF != "B12345678" || pag[0].ClientName != "Cliente SL" {
		t.Fatalf("cliente en página = %q/%q", pag[0].ClientNIF, pag[0].ClientName)
	}
}
