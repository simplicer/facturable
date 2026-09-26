package sello

import (
	"context"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

func newTestService(t *testing.T) (*Service, *fakeSubmitter, *relojFalso, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.Abrir(dir + "/agente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	r := &fakeSubmitter{}
	reloj := &relojFalso{t: time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)}
	srv := &Service{Store: store, Remisor: r, Flujo: application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SistemaLocal{NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
			NombreSistema: "Facturable Sello", IDSistema: "FS", Version: "0.1.0", NumeroInstalacion: "00001"}}
	return srv, r, reloj, dir
}

func TestVerificarCadenaSanaYCorrupta(t *testing.T) {
	srv, _, _, _ := newTestService(t)
	if _, err := srv.IngestCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Cancel("F-2027-0001", "05-01-2027"); err != nil {
		t.Fatalf("Cancel(): %v", err)
	}

	info, err := srv.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Healthy() || info.Registros != 3 {
		t.Fatalf("cadena sana=%v registros=%d rupturas=%v", info.Healthy(), info.Registros, info.Rupturas)
	}

}

func TestAnularYEnviar(t *testing.T) {
	srv, _, reloj, _ := newTestService(t)
	if _, err := srv.IngestCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	id, err := srv.Cancel("F-2027-0002", "05-01-2027")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("id vacío")
	}
	// Tres pendientes: el control de flujo envía uno por ventana de 60 s.
	for i := 0; i < 3; i++ {
		if _, err := srv.SubmitPending(context.Background()); err != nil {
			t.Fatal(err)
		}
		reloj.avanzar(61 * time.Second)
	}
	f, ok, err := srv.Store.ByUUID(pendUUID(t, srv))
	if err != nil || !ok {
		t.Fatal(err)
	}
	if f.Tipo != "ANULACION" || f.Estado != "correcto" {
		t.Fatalf("row anulación = %+v", f)
	}
}

func TestBackupCifradoIdaYVuelta(t *testing.T) {
	srv, _, _, dir := newTestService(t)
	if _, err := srv.IngestCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Store.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	dbPath := dir + "/agente.sqlite"
	bak := dir + "/libro.fbk"
	if err := CreateBackup(dbPath, bak, "clave-secreta"); err != nil {
		t.Fatalf("CreateBackup(): %v", err)
	}
	dest := dir + "/restaurada.sqlite"
	if err := RestoreBackup(bak, dest, "clave-secreta"); err != nil {
		t.Fatalf("RestoreBackup(): %v", err)
	}
	store2, err := persistence.Abrir(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	filas, err := store2.Ledger("B72877814", 0, 1_000_000, 10)
	if err != nil || len(filas) != 2 {
		t.Fatalf("restaurada = %d filas err=%v", len(filas), err)
	}
	if err := RestoreBackup(bak, dest+".x", "clave-mal"); err == nil {
		t.Fatal("contraseña errónea aceptada")
	}
}

// pendUUID devuelve el uuid de la anulación en el libro (helper del test).
func pendUUID(t *testing.T, srv *Service) string {
	t.Helper()
	filas, err := srv.Store.Ledger(srv.EmisorNIF, 0, 1_000_000, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range filas {
		if f.Tipo == "ANULACION" {
			return f.UUID
		}
	}
	t.Fatal("sin anulación en el libro")
	return ""
}
