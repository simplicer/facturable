package persistence

import (
	"fmt"
	"time"
)

// FrequentClient is one entry of the automatic client memory: identity
// plus how many ledger records already use it. It feeds form auto-fill.
type FrequentClient struct {
	NIF    string `json:"nif"`
	Nombre string `json:"nombre"`
	Usos   int64  `json:"usos"`
}

// UpsertClient remembers a client for form auto-fill. Records without a
// client (empty NIF or name) are skipped: the memory fills itself but
// never blocks an ingest. A repeated client refreshes its name (latest
// seen wins) keeping the first-seen timestamp.
func (s *Store) UpsertClient(nif, nombre string) error {
	if nif == "" || nombre == "" {
		return nil
	}
	_, err := s.db.Exec(`
INSERT INTO clientes (nif, nombre, created_at) VALUES (?,?,?)
ON CONFLICT(nif) DO UPDATE SET nombre = excluded.nombre;`,
		nif, nombre, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("no se pudo recordar al cliente %s: %w", nif, err)
	}
	return nil
}

// FrequentClients returns up to limite clients ranked by how many ledger
// records use them (COUNT over registros), most used first. Ties break by
// first-seen date and then NIF so the ranking is deterministic.
func (s *Store) FrequentClients(limite int) ([]FrequentClient, error) {
	rows, err := s.db.Query(`
SELECT c.nif, c.nombre, COUNT(r.id) AS usos
FROM clientes c
LEFT JOIN registros r ON r.cliente_nif = c.nif
GROUP BY c.nif, c.nombre
ORDER BY usos DESC, c.created_at DESC, c.nif
LIMIT ?;`, limite)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la memoria de clientes: %w", err)
	}
	defer rows.Close()
	var out []FrequentClient
	for rows.Next() {
		var c FrequentClient
		if err := rows.Scan(&c.NIF, &c.Nombre, &c.Usos); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
