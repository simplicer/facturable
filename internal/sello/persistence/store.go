// Package persistence keeps the agent billing-record ledger in SQLite.
// Fiscal records are IMMUTABLE (triggers abort UPDATE/DELETE, the RRSIF
// integrity requirement); submission state lives in an append-only log of
// attempts.
package persistence

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

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
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS empresas(
  nif TEXT PRIMARY KEY,
  nombre TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS registros(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT UNIQUE,
  nif TEXT NOT NULL,
  series TEXT NOT NULL,
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
CREATE TRIGGER IF NOT EXISTS registros_inmutables_update
BEFORE UPDATE ON registros
BEGIN SELECT RAISE(ABORT, 'los registros de facturación son inmutables'); END;
CREATE TRIGGER IF NOT EXISTS registros_inmutables_delete
BEFORE DELETE ON registros
BEGIN SELECT RAISE(ABORT, 'los registros de facturación son inmutables'); END;
	CREATE TABLE IF NOT EXISTS ingest_batches(
	  uuid TEXT PRIMARY KEY,
	  body_hash TEXT NOT NULL,
	  response TEXT NOT NULL,
	  created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS clientes(
	  nif TEXT PRIMARY KEY,
	  nombre TEXT,
	  created_at TEXT NOT NULL
	);
CREATE TABLE IF NOT EXISTS remisiones(
  registro_id INTEGER NOT NULL REFERENCES registros(id),
  intento INTEGER NOT NULL,
  estado TEXT NOT NULL,
  csv TEXT,
  codigo TEXT,
  detalle TEXT,
  fecha TEXT NOT NULL,
  PRIMARY KEY (registro_id, intento)
);
CREATE INDEX IF NOT EXISTS idx_registros_orden ON registros(nif, id);`); err != nil {
		return err
	}
	// soft migration for ledgers created before record UUIDs existed
	_, _ = s.db.Exec(`ALTER TABLE registros ADD COLUMN uuid TEXT`)
	// soft migration: which client each record bills (feeds the automatic
	// client memory; older rows stay NULL and simply rank as unused)
	_, _ = s.db.Exec(`ALTER TABLE registros ADD COLUMN cliente_nif TEXT`)
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_registros_uuid ON registros(uuid) WHERE uuid IS NOT NULL`); err != nil {
		// index already exists
		_ = err
	}
	return nil
}

// StoredRecord es un chainRow listo para sellar en el libro.
type StoredRecord struct {
	UUID                                   string
	NIF, Serie, Date, Tipo, Generado       string
	Cuota, Importe, Huella, XML            string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string // vacíos si es el primero
	ClientNIF                              string // destinatario, vacío si no consta
}

// Append añade un chainRow al libro (append-only).
func (s *Store) Append(r StoredRecord) (int64, error) {
	if r.UUID == "" {
		r.UUID = uuid.NewString()
	}
	res, err := s.db.Exec(`INSERT INTO registros
(uuid, nif, series, fecha, tipo, generado, cuota, importe, huella, enlace_serie, enlace_fecha, enlace_huella, xml, creado_en, cliente_nif)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);`,
		nullIfEmpty(r.UUID), r.NIF, r.Serie, r.Date, r.Tipo, r.Generado, r.Cuota, r.Importe, r.Huella,
		nullIfEmpty(r.EnlaceSerie), nullIfEmpty(r.EnlaceFecha), nullIfEmpty(r.EnlaceHuella),
		r.XML, time.Now().UTC().Format(time.RFC3339), nullIfEmpty(r.ClientNIF))
	if err != nil {
		return 0, fmt.Errorf("no se pudo storeRow el chainRow: %w", err)
	}
	return res.LastInsertId()
}

// Last devuelve identidad y enlace del último chainRow de un obligado.
// ok=false cuando la cadena aún no tiene registros.
func (s *Store) Last(nif string) (series, fecha, huella, enlSerie, enlFecha, enlHuella string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT series, fecha, huella, enlace_serie, enlace_fecha, enlace_huella
FROM registros WHERE nif=? ORDER BY id DESC LIMIT 1;`, nif)
	var es, ef, eh sql.NullString
	if err := row.Scan(&series, &fecha, &huella, &es, &ef, &eh); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", "", "", "", false, nil
		}
		return "", "", "", "", "", "", false, fmt.Errorf("no se pudo leer el último chainRow: %w", err)
	}
	return series, fecha, huella, es.String, ef.String, eh.String, true, nil
}

// Previous devuelve series/fecha/huella del penúltimo chainRow (n-2) de un
// obligado; ok=false cuando la cadena tiene un solo chainRow o ninguno.
// Es la pieza que alimenta domain.VerificarSalto antes de sellar.
func (s *Store) Previous(nif string) (series, fecha, huella string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT series, fecha, huella
FROM registros WHERE nif=? ORDER BY id DESC LIMIT 1 OFFSET 1;`, nif)
	if err := row.Scan(&series, &fecha, &huella); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", false, nil
		}
		return "", "", "", false, fmt.Errorf("no se pudo leer el chainRow anterior: %w", err)
	}
	return series, fecha, huella, true, nil
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
	Date  string
	XML   string
}

// Pending devuelve los registros aún sin remitir (sin intento válido).
// Los rechazos no reintentables requieren subsanación manual: no se
// reenvían solos.
// HasSeries reports whether a record with that series already exists for
// the taxpayer (relay/API idempotency: repeated webhooks must not stamp
// twice).
func (s *Store) HasSeries(nif, series string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM registros WHERE nif=? AND series=? LIMIT 1;`, nif, series).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) Pending(nif string, limite int) ([]PendingRecord, error) {
	rows, err := s.db.Query(`
SELECT r.id, r.series, r.fecha, r.xml
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
		if err := rows.Scan(&p.ID, &p.Serie, &p.Date, &p.XML); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ByID devuelve la row del libro de un chainRow concreto.
// ByUUID returns the ledger row of one record by its public UUID (SQL
// direct lookup; audit: internal ids never cross the API).
func (s *Store) ByUUID(recordUUID string) (LedgerRow, bool, error) {
	var f LedgerRow
	err := s.db.QueryRow(`SELECT r.uuid, r.id, r.nif, r.series, r.fecha, r.tipo, r.cuota, r.importe, r.huella,
	       COALESCE(e.estado,'pendiente'), COALESCE(e.csv,''), COALESCE(e.fecha,'')
FROM registros r
LEFT JOIN remisiones e ON e.registro_id = r.id AND e.intento = (
  SELECT MAX(intento) FROM remisiones WHERE registro_id = r.id)
WHERE r.uuid=?;`, recordUUID).Scan(&f.UUID, &f.ID, &f.NIF, &f.Serie, &f.Date, &f.Tipo,
		&f.Cuota, &f.Importe, &f.Huella, &f.Estado, &f.CSV, &f.Remitido)
	if err == sql.ErrNoRows {
		return LedgerRow{}, false, nil
	}
	return f, err == nil, err
}

// XMLByUUID returns the stored submission XML of one record by its public
// UUID. The fiscal fields beyond the ledger columns (client, concept, VAT
// breakdown) live only inside the XML; the duplicate-last flow reads them
// from here.
func (s *Store) XMLByUUID(recordUUID string) (string, bool, error) {
	var xmlStr string
	err := s.db.QueryRow(`SELECT xml FROM registros WHERE uuid=?;`, recordUUID).Scan(&xmlStr)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("no se pudo leer el XML del chainRow: %w", err)
	}
	return xmlStr, true, nil
}

// ChainRecord es todo lo necesario para verificar la cadena completa.
type ChainRecord struct {
	ID                                     int64
	Serie, Date, Tipo, Generado            string
	Cuota, Importe, Huella                 string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string
}

// FullChain devuelve los registros de un obligado en orderSeen de sellado.
func (s *Store) FullChain(nif string) ([]ChainRecord, error) {
	rows, err := s.db.Query(`
SELECT id, series, fecha, tipo, generado, cuota, importe, huella,
       COALESCE(enlace_serie,''), COALESCE(enlace_fecha,''), COALESCE(enlace_huella,'')
FROM registros WHERE nif=? ORDER BY id;`, nif)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la cadena: %w", err)
	}
	defer rows.Close()
	var out []ChainRecord
	for rows.Next() {
		var r ChainRecord
		if err := rows.Scan(&r.ID, &r.Serie, &r.Date, &r.Tipo, &r.Generado,
			&r.Cuota, &r.Importe, &r.Huella, &r.EnlaceSerie, &r.EnlaceFecha, &r.EnlaceHuella); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LedgerRow es la vista del libro para la UI y la exportación.
type LedgerRow struct {
	UUID                   string `json:"uuid"`
	ID                     int64  `json:"-"`
	NIF, Serie, Date, Tipo string
	Cuota, Importe, Huella string
	Estado                 string
	CSV                    string
	Remitido               string
}

// Ledger devuelve el libro ordenado por inserción con el último estado.
func (s *Store) Ledger(nif string, desde, hasta int64, limite int) ([]LedgerRow, error) {
	rows, err := s.db.Query(`
SELECT r.uuid, r.id, r.nif, r.series, r.fecha, r.tipo, r.cuota, r.importe, r.huella,
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
		if err := rows.Scan(&f.UUID, &f.ID, &f.NIF, &f.Serie, &f.Date, &f.Tipo, &f.Cuota, &f.Importe, &f.Huella, &f.Estado, &f.CSV, &f.Remitido); err != nil {
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

// IngestBatch es el resultado persistido de una ingesta idempotente.
type IngestBatch struct {
	UUID, BodyHash, Response string
}

// SaveIngestBatch guarda (o ignora) el resultado de un lote; devuelve el
// resultado previo si el UUID ya existía con otro cuerpo (conflicto).
func (s *Store) SaveIngestBatch(b IngestBatch) (prev *IngestBatch, fresh bool, err error) {
	var hash, resp string
	err = s.db.QueryRow(`SELECT body_hash, response FROM ingest_batches WHERE uuid=?;`, b.UUID).Scan(&hash, &resp)
	if err == nil {
		if hash != b.BodyHash {
			return &IngestBatch{UUID: b.UUID, BodyHash: hash, Response: resp}, false, nil
		}
		return &IngestBatch{UUID: b.UUID, BodyHash: hash, Response: resp}, false, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}
	_, err = s.db.Exec(`INSERT INTO ingest_batches (uuid, body_hash, response, created_at) VALUES (?,?,?,?);`,
		b.UUID, b.BodyHash, b.Response, time.Now().UTC().Format(time.RFC3339))
	return nil, err == nil, err
}

// GetIngestBatch devuelve el lote previo con ese UUID.
func (s *Store) GetIngestBatch(uuid string) (IngestBatch, bool, error) {
	var b IngestBatch
	err := s.db.QueryRow(`SELECT uuid, body_hash, response FROM ingest_batches WHERE uuid=?;`, uuid).
		Scan(&b.UUID, &b.BodyHash, &b.Response)
	if err == sql.ErrNoRows {
		return IngestBatch{}, false, nil
	}
	return b, err == nil, err
}
