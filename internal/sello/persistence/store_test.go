package persistence

import (
	"strings"
	"testing"
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
