package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"facturable/internal/sello"
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

// backupFiles lists the scheduled backup copies of dir, oldest first (the
// timestamp inside the name sorts lexicographically).
func backupFiles(dir string) []string {
	matches, err := filepath.Glob(filepath.Join(dir, "sello-*.fbk"))
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}

// TestCorruptDBDetected: a ledger file that is not a database must be
// classified as corrupt so main answers with the restore instruction
// instead of dying on a silent, cryptic driver error (audit data-loss).
// It exercises the REAL driver error against a garbage file.
func TestCorruptDBDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupto.sqlite")
	if err := os.WriteFile(path, []byte("estos bytes no son una base de datos sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := persistence.Abrir(path)
	if err == nil {
		t.Fatal("Abrir aceptó un fichero corrupto")
	}
	if !corruptDB(err) {
		t.Fatalf("corruptDB(%v) = false, want true", err)
	}
	if corruptDB(nil) {
		t.Fatal("corruptDB(nil) = true")
	}
	if corruptDB(errors.New("no se pudo abrir la base de datos: permiso denegado")) {
		t.Fatal("un error de permisos no es corrupción")
	}
}

// TestScheduledBackupCreaYPoda: every scheduled run writes ONE encrypted
// copy under dir (skipped while the ledger is empty) and prunes the oldest
// copies beyond the retention limit; the newest copy restores into a
// readable ledger with the same passphrase.
func TestScheduledBackupCreaYPoda(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sello.sqlite")
	store, err := persistence.Abrir(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backups := filepath.Join(dir, "backups")

	// empty ledger: nothing to protect yet
	if err := scheduledBackup(store, dbPath, backups, "clave", 5); err != nil {
		t.Fatalf("scheduledBackup con libro vacío: %v", err)
	}
	if n := len(backupFiles(backups)); n != 0 {
		t.Fatalf("copias con libro vacío = %d, want 0", n)
	}

	if _, err := store.Append(persistence.StoredRecord{
		NIF: "B72877814", Serie: "F-2027-0001", Date: "05-01-2027", Tipo: "F1",
		Generado: "2027-01-05T10:00:00+01:00", Cuota: "21.00", Importe: "121.00",
		Huella: strings.Repeat("A", 64), XML: "<x/>",
	}); err != nil {
		t.Fatal(err)
	}
	if err := scheduledBackup(store, dbPath, backups, "clave", 5); err != nil {
		t.Fatalf("scheduledBackup(): %v", err)
	}
	copias := backupFiles(backups)
	if len(copias) != 1 {
		t.Fatalf("copias = %d, want 1", len(copias))
	}
	// the copy restores with the same passphrase into a working ledger
	dest := filepath.Join(dir, "restaurada.sqlite")
	if err := sello.RestoreBackup(copias[0], dest, "clave"); err != nil {
		t.Fatalf("RestoreBackup(): %v", err)
	}
	store2, err := persistence.Abrir(dest)
	if err != nil {
		t.Fatalf("Abrir restaurada: %v", err)
	}
	defer store2.Close()
	if filas, err := store2.Ledger("B72877814", 0, 1_000_000, 10); err != nil || len(filas) != 1 {
		t.Fatalf("libro restaurado = %d filas, %v", len(filas), err)
	}

	// retention: six older-named copies plus the fresh one leave the 5 newest
	for i := 1; i <= 6; i++ {
		vieja := filepath.Join(backups, fmt.Sprintf("sello-202601%02d-000000.fbk", i))
		if err := os.WriteFile(vieja, []byte("copia antigua"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := scheduledBackup(store, dbPath, backups, "clave", 5); err != nil {
		t.Fatalf("scheduledBackup con podas: %v", err)
	}
	copias = backupFiles(backups)
	if len(copias) != 5 {
		t.Fatalf("tras podar quedan %d copias, want 5", len(copias))
	}
	for _, vieja := range []string{
		filepath.Join(backups, "sello-20260101-000000.fbk"),
		filepath.Join(backups, "sello-20260102-000000.fbk"),
	} {
		if _, err := os.Stat(vieja); !os.IsNotExist(err) {
			t.Fatalf("la copia más vieja %s no fue podada", vieja)
		}
	}
}
