package relay

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"database/sql"

	_ "modernc.org/sqlite"
)

func nuevoServidor(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "relay.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	srv, err := NewServer(db)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.Mount(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestCicloRelayClaveSobreAck(t *testing.T) {
	_, ts := nuevoServidor(t)
	cli := ts.Client()

	agente, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pub := base64.StdEncoding.EncodeToString(agente.PublicKey().Bytes())

	// registro de clave
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/keys/B72877814",
		strings.NewReader(`{"pubkey":"`+pub+`"}`))
	if r, err := cli.Do(req); err != nil || r.StatusCode != 204 {
		t.Fatalf("PUT key = %v %v", err, r)
	}
	// pubkey inválida → 422
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/v1/keys/B72877814",
		strings.NewReader(`{"pubkey":"AAAA"}`))
	if r, _ := cli.Do(req); r.StatusCode != 422 {
		t.Fatalf("PUT key inválida = %d, want 422", r.StatusCode)
	}

	// sobre de un remitente (usa Seal real)
	env, err := Seal(agente.PublicKey(), []byte(`[{"numero_serie":"R-1"}]`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(env)
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/v1/envelopes/99999999-9999-9999-9999-999999999999?nif=B72877814",
		strings.NewReader(string(body)))
	if r, err := cli.Do(req); err != nil || r.StatusCode != 201 {
		t.Fatalf("PUT envelope = %v %v", err, r)
	}
	// replay idempotente
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/v1/envelopes/99999999-9999-9999-9999-999999999999?nif=B72877814",
		strings.NewReader(string(body)))
	if r, _ := cli.Do(req); r.StatusCode != 200 {
		t.Fatalf("replay envelope = %d, want 200", r.StatusCode)
	}

	// el agente ve el sobre y lo ack-ea
	if r, err := cli.Get(ts.URL + "/v1/envelopes?nif=B72877814"); err != nil || r.StatusCode != 200 {
		t.Fatalf("list = %v %v", err, r)
	} else {
		var out struct {
			Data []struct {
				ID       string `json:"id"`
				EphPub   string `json:"ephemeral_pub"`
				Nonce    string `json:"nonce"`
				CiphText string `json:"ciphertext"`
			} `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&out)
		if len(out.Data) != 1 {
			t.Fatalf("sobres = %d, want 1", len(out.Data))
		}
		req, _ = http.NewRequest(http.MethodPost, ts.URL+"/v1/envelopes/"+out.Data[0].ID+"/acknowledgements", nil)
		if r, err := cli.Do(req); err != nil || r.StatusCode != 204 {
			t.Fatalf("ack = %v %v", err, r)
		}
	}
	// ackeado: la lista queda vacía
	if r, _ := cli.Get(ts.URL + "/v1/envelopes?nif=B72877814"); r.StatusCode != 200 {
		t.Fatal("list tras ack")
	} else {
		var out struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&out)
		if len(out.Data) != 0 {
			t.Fatalf("sobres tras ack = %d, want 0", len(out.Data))
		}
	}
}

// TestPutKeyValidaNIF pins the audit fix: PUT /v1/keys/{nif} must reject a
// malformed NIF (422) even with a perfectly good public key, and must store
// the canonical (uppercase, trimmed) NIF so later lookups by any casing hit
// the same registry row.
func TestPutKeyValidaNIF(t *testing.T) {
	_, ts := nuevoServidor(t)
	cli := ts.Client()

	agente, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := base64.StdEncoding.EncodeToString(agente.PublicKey().Bytes())
	cuerpo := `{"pubkey":"` + pub + `"}`

	put := func(nif string) int {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/keys/"+nif, strings.NewReader(cuerpo))
		r, _ := cli.Do(req)
		return r.StatusCode
	}
	// well-formed pubkey + garbage NIF: only the NIF can be the reason
	for _, bad := range []string{"hack", "12345678A", "B-7287781-4", "..."} {
		if got := put(bad); got != 422 {
			t.Fatalf("PUT key NIF %q = %d, want 422 (invalid NIF rejected)", bad, got)
		}
	}
	// valid NIF in lowercase registers 204...
	if got := put("b72877814"); got != 204 {
		t.Fatalf("PUT key NIF válido = %d, want 204", got)
	}
	// ...and the key is stored under the canonical form
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/keys/B72877814", nil)
	r, _ := cli.Do(req)
	if r == nil || r.StatusCode != 200 {
		t.Fatalf("GET key canónica = %v, want 200 (NIF normalizado al guardar)", r)
	}
	var out struct {
		Data struct {
			Pubkey string `json:"pubkey"`
		} `json:"data"`
	}
	_ = json.NewDecoder(r.Body).Decode(&out)
	if out.Data.Pubkey != pub {
		t.Fatalf("pubkey canónica = %q, want %q", out.Data.Pubkey, pub)
	}
}
