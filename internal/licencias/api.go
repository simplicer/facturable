// Package licencias: api.go expone el servicio de licencias (PUT idempotente
// con UUID de cliente, :check con la regla nunca-rehén). HTTP + persistencia
// viven aquí como adaptadores; la lógica de firma/validez es la del paquete.
package licencias

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Store de licencias (tabla unica, sqlite).
type store struct{ db *sql.DB }

func nuevaStore(db *sql.DB) (*store, error) {
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

// CargarOGenerarClave lee etc/licencias.ed25519 o crea el par (dev) y
// guarda la privada (0600); devuelve la privada y la pública.
func CargarOGenerarClave(ruta string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
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

// API monta los endpoints de licencias en mux.
type API struct {
	store *store
	priv  ed25519.PrivateKey
	ahora func() time.Time
}

// NuevaAPI prepara el servicio de licencias sobre una base sql abierta.
func NuevaAPI(db *sql.DB, priv ed25519.PrivateKey) (*API, error) {
	st, err := nuevaStore(db)
	if err != nil {
		return nil, err
	}
	return &API{store: st, priv: priv, ahora: time.Now}, nil
}

// Montar registra las rutas (Zalando: PUT idempotente con UUID cliente).
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/licenses/{id}", a.put)
	mux.HandleFunc("GET /v1/licenses/{id}", a.get)
	// Recurso derivado sustantivo en vez de sufijo :check (el mux de Go no
	// admite wildcards con sufijo, y "validity" es más resource-oriented).
	mux.HandleFunc("GET /v1/licenses/{id}/validity", a.validity)
}

type peticionLicencia struct {
	Plan        Plan     `json:"plan"`
	NIFs        []string `json:"nifs"`
	DiasValidez int      `json:"dias_validez"`
	GraceDays   int      `json:"grace_days"`
}

func (a *API) put(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !esUUID(id) {
		problem(w, 400, "invalid license id: a client-provided UUID is required", r)
		return
	}
	var req peticionLicencia
	if err := json.NewDecoder(io1MB(r)).Decode(&req); err != nil {
		problem(w, 400, "unreadable body: "+err.Error(), r)
		return
	}
	body := fmt.Sprintf("%v|%v|%d|%d", req.Plan, req.NIFs, req.DiasValidez, req.GraceDays)

	// idempotencia: misma petición → mismo token; distinta → conflicto
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
		problem(w, 500, err.Error(), r)
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
		problem(w, 500, err.Error(), r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"license_id": id, "token": token}})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	lic, token, ok := a.porID(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"license_id": lic.LicenseID, "plan": lic.Plan, "nifs": lic.NIFs,
		"expires_at": lic.ExpiresAt, "grace_days": lic.GraceDays, "token": token,
	}})
}

// validity es el recurso derivado "validez de la licencia": sellar caduca con
// gracia; abrir/exportar jamás se bloquea (Gate del paquete). Sustantivo, no
// acción: el verbo HTTP (GET) ya es la acción.
func (a *API) validity(w http.ResponseWriter, r *http.Request) {
	lic, _, ok := a.porID(w, r)
	if !ok {
		return
	}
	res := map[string]any{
		"open_data": true, "export": true, // nunca rehén
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

func (a *API) porID(w http.ResponseWriter, r *http.Request) (License, string, bool) {
	id := r.PathValue("id")
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
		problem(w, 500, err.Error(), r)
		return License{}, "", false
	}
	var nifs []string
	_ = json.Unmarshal([]byte(nifsJSON), &nifs)
	ex, _ := time.Parse(time.RFC3339, expires)
	is, _ := time.Parse(time.RFC3339, issued)
	return NewLicense(id, Plan(plan), nifs, is, ex, grace), token, true
}

// esUUID accepts the canonical 8-4-4-4-12 hexadecimal form.
func esUUID(id string) bool {
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

func io1MB(r *http.Request) ioReader { return ioReader{r} }

type ioReader struct{ r *http.Request }

func (i ioReader) Read(p []byte) (int, error) { return i.r.Body.Read(p) }

func problem(w http.ResponseWriter, status int, detail string, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "https://api.facturable.es/problems/licencia", "title": http.StatusText(status),
		"status": status, "detail": detail, "instance": r.URL.Path,
	})
}
