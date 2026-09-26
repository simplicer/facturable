// Server side of the mailbox: key registry per taxpayer and the envelope
// store. Routes follow the naming rules (plural nouns, UUID ids).
package relay

import (
	"crypto/ecdh"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Server stores keys and opaque envelopes.
type Server struct{ db *sql.DB }

// NewServer migrates the relay tables.
func NewServer(db *sql.DB) (*Server, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS relay_keys(
	  nif TEXT PRIMARY KEY, pubkey TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS relay_envelopes(
	  id TEXT PRIMARY KEY, nif TEXT NOT NULL,
	  eph_pub TEXT NOT NULL, nonce TEXT NOT NULL, ciphertext TEXT NOT NULL,
	  created_at TEXT NOT NULL, delivered INTEGER NOT NULL DEFAULT 0
	);`)
	if err != nil {
		return nil, err
	}
	return &Server{db: db}, nil
}

// Mount registers the relay routes.
func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/keys/{nif}", s.putKey)
	mux.HandleFunc("GET /v1/keys/{nif}", s.getKey)
	mux.HandleFunc("POST /v1/envelopes", s.postEnvelope)
	mux.HandleFunc("GET /v1/envelopes", s.listEnvelopes)
	mux.HandleFunc("DELETE /v1/envelopes/{id}", s.deleteEnvelope)
}

func (s *Server) putKey(w http.ResponseWriter, r *http.Request) {
	nif := r.PathValue("nif")
	var body struct {
		Pubkey string `json:"pubkey"` // base64 X25519 public key
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		problem(w, 400, "unreadable body", r)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Pubkey)
	if err != nil || len(raw) != 32 {
		problem(w, 422, "pubkey must be base64 of a 32-byte X25519 public key", r)
		return
	}
	if _, err := ecdh.X25519().NewPublicKey(raw); err != nil {
		problem(w, 422, "pubkey is not a valid X25519 point", r)
		return
	}
	_, _ = s.db.Exec(`INSERT INTO relay_keys (nif, pubkey, updated_at) VALUES (?,?,?)
		ON CONFLICT(nif) DO UPDATE SET pubkey=excluded.pubkey, updated_at=excluded.updated_at;`,
		nif, body.Pubkey, time.Now().UTC().Format(time.RFC3339))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getKey(w http.ResponseWriter, r *http.Request) {
	var pub string
	err := s.db.QueryRow(`SELECT pubkey FROM relay_keys WHERE nif=?;`, r.PathValue("nif")).Scan(&pub)
	if err == sql.ErrNoRows {
		problem(w, 404, "no key registered for this nif", r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"pubkey": pub}})
}

func (s *Server) postEnvelope(w http.ResponseWriter, r *http.Request) {
	var env Envelope
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&env); err != nil {
		problem(w, 400, "unreadable envelope", r)
		return
	}
	nif := r.URL.Query().Get("nif")
	if nif == "" {
		problem(w, 422, "nif query parameter required", r)
		return
	}
	id := uuid.NewString()
	_, err := s.db.Exec(`INSERT INTO relay_envelopes (id, nif, eph_pub, nonce, ciphertext, created_at)
VALUES (?,?,?,?,?,?);`, id, nif,
		base64.StdEncoding.EncodeToString(env.EphemeralPub),
		base64.StdEncoding.EncodeToString(env.Nonce),
		base64.StdEncoding.EncodeToString(env.Ciphertext),
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		problem(w, 500, err.Error(), r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": id}})
}

// listEnvelopes returns undelivered envelopes for a nif (oldest first).
func (s *Server) listEnvelopes(w http.ResponseWriter, r *http.Request) {
	nif := r.URL.Query().Get("nif")
	if nif == "" {
		problem(w, 422, "nif query parameter required", r)
		return
	}
	rows, err := s.db.Query(`SELECT id, eph_pub, nonce, ciphertext FROM relay_envelopes
WHERE nif=? AND delivered=0 ORDER BY created_at LIMIT 100;`, nif)
	if err != nil {
		problem(w, 500, err.Error(), r)
		return
	}
	defer rows.Close()
	type item struct {
		ID           string `json:"id"`
		EphemeralPub string `json:"ephemeral_pub"`
		Nonce        string `json:"nonce"`
		Ciphertext   string `json:"ciphertext"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var ep, no, ci string
		if err := rows.Scan(&it.ID, &ep, &no, &ci); err != nil {
			problem(w, 500, err.Error(), r)
			return
		}
		it.EphemeralPub, it.Nonce, it.Ciphertext = ep, no, ci
		out = append(out, it)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

func (s *Server) deleteEnvelope(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.Exec(`UPDATE relay_envelopes SET delivered=1 WHERE id=?;`, r.PathValue("id"))
	if err != nil {
		problem(w, 500, err.Error(), r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Purge removes delivered and expired envelopes (call from a ticker).
func (s *Server) Purge() {
	cutoff := time.Now().UTC().Add(-TTL).Format(time.RFC3339)
	_, _ = s.db.Exec(`DELETE FROM relay_envelopes WHERE delivered=1 OR created_at < ?;`, cutoff)
}

func problem(w http.ResponseWriter, status int, detail string, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title": http.StatusText(status), "status": status, "detail": detail, "instance": r.URL.Path,
	})
}
