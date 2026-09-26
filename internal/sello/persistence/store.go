// Package persistence keeps the agent billing-record ledger in SQLite.
// Fiscal records are IMMUTABLE (triggers abort UPDATE/DELETE, the RRSIF
// integrity requirement); submission state lives in an append-only log of
// attempts.
package persistence

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the SIF local ledger.
type Store struct{ db *sql.DB }

// Open initializes (or reuses) the database at path.
func Abrir(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la base de datos: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: una escritora evita SQLITE_BUSY
	s := &Store{db: db}
	if err := s.migrar(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Checkpoint flushes the WAL into the main file (required before copying
// the database file, e.g. for a backup).
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

func (s *Store) migrar() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS empresas(
  nif TEXT PRIMARY KEY,
  nombre TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS registros(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  nif TEXT NOT NULL,
  serie TEXT NOT NULL,
  fecha TEXT NOT NULL,
  tipo TEXT NOT NULL,
  generado TEXT NOT NULL,
  cuota TEXT NOT NULL,
  importe TEXT NOT NULL,
  huella TEXT NOT NULL UNIQUE,
  enlace_serie TEXT,
  enlace_fecha TEXT,
  enlace_huella TEXT,
  xml TEXT NOT NULL,
  creado_en TEXT NOT NULL
);
-- Integridad RRSIF: un chainRow de facturación jamás se modifica ni borra.
CREATE TRIGGER IF NOT EXISTS registros_inmutables_update
BEFORE UPDATE ON registros
BEGIN SELECT RAISE(ABORT, 'los registros de facturación son inmutables'); END;
CREATE TRIGGER IF NOT EXISTS registros_inmutables_delete
BEFORE DELETE ON registros
BEGIN SELECT RAISE(ABORT, 'los registros de facturación son inmutables'); END;
-- Log append-only de intentos de remisión (el "estado" es el último intento).
CREATE TABLE IF NOT EXISTS remisiones(
  registro_id INTEGER NOT NULL REFERENCES registros(id),
  intento INTEGER NOT NULL,
  estado TEXT NOT NULL,          -- pendiente|correcto|aceptado_errores|rechazado
  csv TEXT,
  codigo TEXT,
  detalle TEXT,
  fecha TEXT NOT NULL,
  PRIMARY KEY (registro_id, intento)
);
CREATE INDEX IF NOT EXISTS idx_registros_orden ON registros(nif, id);
`)
	return err
}

// StoredRecord es un chainRow listo para sellar en el libro.
type StoredRecord struct {
	NIF, Serie, Fecha, Tipo, Generado      string
	Cuota, Importe, Huella, XML            string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string // vacíos si es el primero
}

// Append añade un chainRow al libro (append-only).
func (s *Store) Append(r StoredRecord) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO registros
(nif, serie, fecha, tipo, generado, cuota, importe, huella, enlace_serie, enlace_fecha, enlace_huella, xml, creado_en)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?);`,
		r.NIF, r.Serie, r.Fecha, r.Tipo, r.Generado, r.Cuota, r.Importe, r.Huella,
		nullIfEmpty(r.EnlaceSerie), nullIfEmpty(r.EnlaceFecha), nullIfEmpty(r.EnlaceHuella),
		r.XML, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("no se pudo storeRow el chainRow: %w", err)
	}
	return res.LastInsertId()
}

// Last devuelve identidad y enlace del último chainRow de un obligado.
// ok=false cuando la cadena aún no tiene registros.
func (s *Store) Last(nif string) (serie, fecha, huella, enlSerie, enlFecha, enlHuella string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT serie, fecha, huella, enlace_serie, enlace_fecha, enlace_huella
FROM registros WHERE nif=? ORDER BY id DESC LIMIT 1;`, nif)
	var es, ef, eh sql.NullString
	if err := row.Scan(&serie, &fecha, &huella, &es, &ef, &eh); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", "", "", "", false, nil
		}
		return "", "", "", "", "", "", false, fmt.Errorf("no se pudo leer el último chainRow: %w", err)
	}
	return serie, fecha, huella, es.String, ef.String, eh.String, true, nil
}

// Previous devuelve serie/fecha/huella del penúltimo chainRow (n-2) de un
// obligado; ok=false cuando la cadena tiene un solo chainRow o ninguno.
// Es la pieza que alimenta domain.VerificarSalto antes de sellar.
func (s *Store) Previous(nif string) (serie, fecha, huella string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT serie, fecha, huella
FROM registros WHERE nif=? ORDER BY id DESC LIMIT 1 OFFSET 1;`, nif)
	if err := row.Scan(&serie, &fecha, &huella); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", false, nil
		}
		return "", "", "", false, fmt.Errorf("no se pudo leer el chainRow anterior: %w", err)
	}
	return serie, fecha, huella, true, nil
}

// Submission
type StoredSubmission struct {
	RegistroID           int64
	Estado               string // correcto|aceptado_errores|rechazado
	CSV, Codigo, Detalle string
}

// RecordSubmission añade un intento al log.
func (s *Store) RecordSubmission(r StoredSubmission) error {
	var intento int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(intento),0)+1 FROM remisiones WHERE registro_id=?;`, r.RegistroID).Scan(&intento); err != nil {
		return fmt.Errorf("no se pudo numerar el intento: %w", err)
	}
	_, err := s.db.Exec(`INSERT INTO remisiones (registro_id, intento, estado, csv, codigo, detalle, fecha) VALUES (?,?,?,?,?,?,?);`,
		r.RegistroID, intento, r.Estado, nullIfEmpty(r.CSV), nullIfEmpty(r.Codigo), nullIfEmpty(r.Detalle),
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// PendingRecord es un chainRow esperando remisión.
type PendingRecord struct {
	ID    int64
	Serie string
	Fecha string
	XML   string
}

// Pending devuelve los registros aún sin remitir (sin intento válido).
// Los rechazos no reintentables requieren subsanación manual: no se
// reenvían solos.
func (s *Store) Pending(nif string, limite int) ([]PendingRecord, error) {
	rows, err := s.db.Query(`
SELECT r.id, r.serie, r.fecha, r.xml
FROM registros r
WHERE r.nif = ?
  AND NOT EXISTS (
    SELECT 1 FROM remisiones e WHERE e.registro_id = r.id
      AND e.estado IN ('correcto','aceptado_errores','rechazado'))
ORDER BY r.id LIMIT ?;`, nif, limite)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer pendientes: %w", err)
	}
	defer rows.Close()
	var out []PendingRecord
	for rows.Next() {
		var p PendingRecord
		if err := rows.Scan(&p.ID, &p.Serie, &p.Fecha, &p.XML); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ByID devuelve la fila del libro de un chainRow concreto.
func (s *Store) ByID(id int64) (LedgerRow, bool, error) {
	filas, err := s.Ledger("", 0, 1_000_000_000, 1_000_000)
	if err != nil {
		return LedgerRow{}, false, err
	}
	for _, f := range filas {
		if f.ID == id {
			return f, true, nil
		}
	}
	return LedgerRow{}, false, nil
}

// ChainRecord es todo lo necesario para verificar la cadena completa.
type ChainRecord struct {
	ID                                     int64
	Serie, Fecha, Tipo, Generado           string
	Cuota, Importe, Huella                 string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string
}

// FullChain devuelve los registros de un obligado en orden de sellado.
func (s *Store) FullChain(nif string) ([]ChainRecord, error) {
	rows, err := s.db.Query(`
SELECT id, serie, fecha, tipo, generado, cuota, importe, huella,
       COALESCE(enlace_serie,''), COALESCE(enlace_fecha,''), COALESCE(enlace_huella,'')
FROM registros WHERE nif=? ORDER BY id;`, nif)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la cadena: %w", err)
	}
	defer rows.Close()
	var out []ChainRecord
	for rows.Next() {
		var r ChainRecord
		if err := rows.Scan(&r.ID, &r.Serie, &r.Fecha, &r.Tipo, &r.Generado,
			&r.Cuota, &r.Importe, &r.Huella, &r.EnlaceSerie, &r.EnlaceFecha, &r.EnlaceHuella); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LedgerRow es la vista del libro para la UI y la exportación.
type LedgerRow struct {
	ID                      int64
	NIF, Serie, Fecha, Tipo string
	Cuota, Importe, Huella  string
	Estado                  string
	CSV                     string
	Remitido                string
}

// Ledger devuelve el libro ordenado por inserción con el último estado.
func (s *Store) Ledger(nif string, desde, hasta int64, limite int) ([]LedgerRow, error) {
	rows, err := s.db.Query(`
SELECT r.id, r.nif, r.serie, r.fecha, r.tipo, r.cuota, r.importe, r.huella,
       COALESCE(e.estado,'pendiente'),
       COALESCE(e.csv, (SELECT csv FROM remisiones
                         WHERE registro_id = r.id AND csv IS NOT NULL
                         ORDER BY intento DESC LIMIT 1), ''),
       COALESCE(e.fecha,'')
FROM registros r
LEFT JOIN remisiones e ON e.registro_id = r.id AND e.intento = (
  SELECT MAX(intento) FROM remisiones WHERE registro_id = r.id)
WHERE (? = '' OR r.nif = ?) AND r.id BETWEEN ? AND ?
ORDER BY r.id LIMIT ?;`, nif, nif, desde, hasta, limite)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer el libro: %w", err)
	}
	defer rows.Close()
	var out []LedgerRow
	for rows.Next() {
		var f LedgerRow
		if err := rows.Scan(&f.ID, &f.NIF, &f.Serie, &f.Fecha, &f.Tipo, &f.Cuota, &f.Importe, &f.Huella, &f.Estado, &f.CSV, &f.Remitido); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
