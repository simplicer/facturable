package sello

import (
	"context"
	"strings"
	"testing"
	"time"

	"facturable/internal/sello/persistencia"
	"facturable/internal/verifactu/application"
)

func servicioPrueba(t *testing.T) (*Servicio, *remisorSimulado, *relojFalso, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistencia.Abrir(dir + "/agente.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	r := &remisorSimulado{}
	reloj := &relojFalso{t: time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC)}
	srv := &Servicio{Store: store, Remisor: r, Flujo: application.NewControlFlujo(reloj.ahora),
		EmisorNIF: "B72877814", EmisorNombre: "Facturable Test SL",
		Sistema: application.SistemaLocal{NombreProductor: "Facturable Test SL", NIFProductor: "B72877814",
			NombreSistema: "Facturable Sello", IDSistema: "FS", Version: "0.1.0", NumeroInstalacion: "00001"}}
	return srv, r, reloj, dir
}

func TestVerificarCadenaSanaYCorrupta(t *testing.T) {
	srv, _, _, _ := servicioPrueba(t)
	if _, err := srv.IngestaCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Anular("F-2027-0001", "05-01-2027"); err != nil {
		t.Fatalf("Anular(): %v", err)
	}

	info, err := srv.VerificarCadena()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Sana() || info.Registros != 3 {
		t.Fatalf("cadena sana=%v registros=%d rupturas=%v", info.Sana(), info.Registros, info.Rupturas)
	}

}

func TestAnularYEnviar(t *testing.T) {
	srv, _, reloj, _ := servicioPrueba(t)
	if _, err := srv.IngestaCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	id, err := srv.Anular("F-2027-0002", "05-01-2027")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("id vacío")
	}
	// Tres pendientes: el control de flujo envía uno por ventana de 60 s.
	for i := 0; i < 3; i++ {
		if _, err := srv.EnviarPendientes(context.Background()); err != nil {
			t.Fatal(err)
		}
		reloj.avanzar(61 * time.Second)
	}
	f, ok, err := srv.Store.PorID(id)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if f.Tipo != "ANULACION" || f.Estado != "correcto" {
		t.Fatalf("fila anulación = %+v", f)
	}
}

func TestBackupCifradoIdaYVuelta(t *testing.T) {
	srv, _, _, dir := servicioPrueba(t)
	if _, err := srv.IngestaCSV(strings.NewReader(csvPrueba)); err != nil {
		t.Fatal(err)
	}
	if err := srv.Store.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	dbPath := dir + "/agente.sqlite"
	bak := dir + "/libro.fbk"
	if err := CrearBackup(dbPath, bak, "clave-secreta"); err != nil {
		t.Fatalf("CrearBackup(): %v", err)
	}
	dest := dir + "/restaurada.sqlite"
	if err := RestaurarBackup(bak, dest, "clave-secreta"); err != nil {
		t.Fatalf("RestaurarBackup(): %v", err)
	}
	store2, err := persistencia.Abrir(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	filas, err := store2.Listado("B72877814", 0, 1_000_000, 10)
	if err != nil || len(filas) != 2 {
		t.Fatalf("restaurada = %d filas err=%v", len(filas), err)
	}
	if err := RestaurarBackup(bak, dest+".x", "clave-mal"); err == nil {
		t.Fatal("contraseña errónea aceptada")
	}
}
