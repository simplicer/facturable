// Package licencias: api.go serves the license API (idempotent PUT with
// client UUID, never-hostage validity). HTTP and persistence live here as
// adapters; signing/validity logic belongs to the package core.
package licenses

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Store de licencias (tabla unica, sqlite).
type store struct{ db *sql.DB }

func newStore(db *sql.DB) (*store, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS licencias(
	  id TEXT PRIMARY KEY,
	  plan TEXT NOT NULL,
	  nifs TEXT NOT NULL,
	  expires_at TEXT NOT NULL,
	  grace_days INTEGER NOT NULL,
	  issued_at TEXT NOT NULL,
	  token TEXT NOT NULL,
	  body TEXT NOT NULL
	);`)
	if err != nil {
		return nil, err
	}
	return &store{db: db}, nil
}

// LoadOrCreateKey reads etc/licencias.ed25519 or creates the keypair (dev)
// and stores the private key (0600); it returns private and public.
func LoadOrCreateKey(ruta string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if raw, err := os.ReadFile(ruta); err == nil && len(raw) == ed25519.PrivateKeySize {
		priv := ed25519.PrivateKey(raw)
		return priv, priv.Public().(ed25519.PublicKey), nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(ruta), 0o700); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(ruta, priv, 0o600); err != nil {
		return nil, nil, err
	}
	_ = os.WriteFile(ruta+".pub", pub, 0o644)
	return priv, pub, nil
}

// API holds the license service.
type API struct {
	store *store
	priv  ed25519.PrivateKey
	ahora func() time.Time
}

// NewAPI builds the license service on an open sql DB.
func NewAPI(db *sql.DB, priv ed25519.PrivateKey) (*API, error) {
	st, err := newStore(db)
	if err != nil {
		return nil, err
	}
	return &API{store: st, priv: priv, ahora: time.Now}, nil
}

// Montar registra las rutas (Zalando: PUT idempotente con UUID cliente).
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/licenses/{id}", a.put)
	mux.HandleFunc("GET /v1/licenses/{id}", a.get)
	// Derived noun resource instead of a :check suffix (the Go mux cannot
	// carry suffixes after a wildcard, and "validity" is more
	// resource-oriented).
	mux.HandleFunc("GET /v1/licenses/{id}/validity", a.validity)
}

type licenseRequest struct {
	Plan        Plan     `json:"plan"`
	NIFs        []string `json:"nifs"`
	DiasValidez int      `json:"dias_validez"`
	GraceDays   int      `json:"grace_days"`
}

func (a *API) put(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		problem(w, 400, "invalid license id: a client-provided UUID is required", r)
		return
	}
	var req licenseRequest
	if err := json.NewDecoder(limitedBody(r)).Decode(&req); err != nil {
		problem(w, 400, "unreadable body: "+err.Error(), r)
		return
	}
	body := fmt.Sprintf("%v|%v|%d|%d", req.Plan, req.NIFs, req.DiasValidez, req.GraceDays)

	// idempotency: same request yields the same token; a different one conflicts
	var tokenExistente, bodyExistente string
	err := a.store.db.QueryRow(`SELECT token, body FROM licencias WHERE id=?;`, id).Scan(&tokenExistente, &bodyExistente)
	switch {
	case err == nil:
		if bodyExistente != body {
			problem(w, 409, "license already exists with different data", r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"license_id": id, "token": tokenExistente}})
		return
	case !errors.Is(err, sql.ErrNoRows):
		internalProblem(w, r, err)
		return
	}

	if req.DiasValidez <= 0 || req.DiasValidez > 400 {
		problem(w, 422, "dias_validez must be between 1 and 400", r)
		return
	}
	ahora := a.ahora().UTC()
	lic := NewLicense(id, req.Plan, req.NIFs, ahora, ahora.AddDate(0, 0, req.DiasValidez), req.GraceDays)
	token, err := Sign(a.priv, lic)
	if err != nil {
		problem(w, 422, err.Error(), r)
		return
	}
	_, err = a.store.db.Exec(`INSERT INTO licencias (id, plan, nifs, expires_at, grace_days, issued_at, token, body)
VALUES (?,?,?,?,?,?,?,?);`, id, string(req.Plan), marshalNIFs(req.NIFs),
		lic.ExpiresAt.Format(time.RFC3339), lic.GraceDays, lic.IssuedAt.Format(time.RFC3339), token, body)
	if err != nil {
		internalProblem(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"license_id": id, "token": token}})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	lic, token, ok := a.findByID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"license_id": lic.LicenseID, "plan": lic.Plan, "nifs": lic.NIFs,
		"expires_at": lic.ExpiresAt, "grace_days": lic.GraceDays, "token": token,
	}})
}

// validity is the derived "license validity" resource: stamping expires
// with grace; open/export are never blocked (the package Gate). A noun, not
// an action: the HTTP verb (GET) already is the action.
func (a *API) validity(w http.ResponseWriter, r *http.Request) {
	lic, _, ok := a.findByID(w, r)
	if !ok {
		return
	}
	res := map[string]any{
		"open_data": true, "export": true, // never a hostage
	}
	if err := Gate(lic, a.ahora(), ActionStampNew); err != nil {
		res["stamp_new"] = false
		res["motivo"] = err.Error()
	} else {
		res["stamp_new"] = true
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": res})
}

func (a *API) findByID(w http.ResponseWriter, r *http.Request) (License, string, bool) {
	id := r.PathValue("id")
	// Strict UUID validation on EVERY path that carries an id (audit):
	// GET /v1/licenses/{id} and GET /v1/licenses/{id}/validity must reject
	// non-UUID ids exactly like PUT does.
	if !isUUID(id) {
		problem(w, 400, "invalid license id: a client-provided UUID is required", r)
		return License{}, "", false
	}
	var plan, nifsJSON, token string
	var expires, issued string
	var grace int
	err := a.store.db.QueryRow(`SELECT plan, nifs, expires_at, grace_days, issued_at, token FROM licencias WHERE id=?;`, id).
		Scan(&plan, &nifsJSON, &expires, &grace, &issued, &token)
	if errors.Is(err, sql.ErrNoRows) {
		problem(w, 404, "license not found", r)
		return License{}, "", false
	}
	if err != nil {
		internalProblem(w, r, err)
		return License{}, "", false
	}
	var nifs []string
	_ = json.Unmarshal([]byte(nifsJSON), &nifs)
	ex, _ := time.Parse(time.RFC3339, expires)
	is, _ := time.Parse(time.RFC3339, issued)
	return NewLicense(id, Plan(plan), nifs, is, ex, grace), token, true
}

// isUUID accepts the canonical 8-4-4-4-12 hexadecimal form.
func isUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func marshalNIFs(nifs []string) string {
	b, _ := json.Marshal(nifs)
	return string(b)
}

func limitedBody(r *http.Request) bodyReader { return bodyReader{r} }

type bodyReader struct{ r *http.Request }

func (i bodyReader) Read(p []byte) (int, error) { return i.r.Body.Read(p) }

// internalProblem answers a 500 with a generic detail: the storage cause is
// logged server-side and its SQL text never reaches the client (audit).
func internalProblem(w http.ResponseWriter, r *http.Request, cause error) {
	log.Printf("licenses api: %v", cause)
	problem(w, 500, "error interno; inténtalo de nuevo", r)
}

func problem(w http.ResponseWriter, status int, detail string, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "https://api.facturable.es/problems/licencia", "title": http.StatusText(status),
		"status": status, "detail": detail, "instance": r.URL.Path,
	})
}
