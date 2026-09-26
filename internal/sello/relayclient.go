package sello

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"facturable/internal/relay"

	"facturable/internal/sello/ingest"
)

// RelayClient pulls encrypted envelopes from the public API and ingests
// them locally. The private key never leaves this machine.
// RelayClient needs the parent Service to stamp: set Service after building.
type RelayClient struct {
	Service *Service

	APIBase string // e.g. https://api.facturable.es
	NIF     string
	priv    *ecdh.PrivateKey
	http    *http.Client
}

// LoadOrCreateRelayKey keeps the agent X25519 key next to the database.
func LoadOrCreateRelayKey(dir string) (*ecdh.PrivateKey, error) {
	path := filepath.Join(dir, "relay-x25519.key")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		return ecdh.X25519().NewPrivateKey(raw)
	}
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, k.Bytes(), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

// NewRelayClient builds the polling client.
func NewRelayClient(apiBase, nif string, priv *ecdh.PrivateKey) *RelayClient {
	return &RelayClient{APIBase: apiBase, NIF: nif, priv: priv, http: &http.Client{Timeout: 15 * time.Second}}
}

// PublicKeyB64 returns the base64 public key to register with the API.
func (c *RelayClient) PublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(c.priv.PublicKey().Bytes())
}

// Register publishes (idempotent PUT) the agent public key for its NIF.
func (c *RelayClient) Register(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"pubkey": c.PublicKeyB64()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.APIBase+"/v1/keys/"+c.NIF, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("key register: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Pull fetches pending envelopes, decrypts and ingests each one, then acks.
// Returns ingested count and first error (processing continues on error so
// one bad envelope does not block the mailbox).
func (c *RelayClient) Pull(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.APIBase+"/v1/envelopes?nif="+c.NIF, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("envelope list: HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID           string `json:"id"`
			EphemeralPub string `json:"ephemeral_pub"`
			Nonce        string `json:"nonce"`
			Ciphertext   string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}

	if c.Service == nil {
		return 0, fmt.Errorf("relay client has no Service wired")
	}
	ingested := 0
	for _, it := range payload.Data {
		ep, _ := base64.StdEncoding.DecodeString(it.EphemeralPub)
		no, _ := base64.StdEncoding.DecodeString(it.Nonce)
		ci, _ := base64.StdEncoding.DecodeString(it.Ciphertext)
		plain, err := relay.Open(c.priv, &relay.Envelope{EphemeralPub: ep, Nonce: no, Ciphertext: ci})
		if err != nil {
			c.ack(ctx, it.ID) // undecryptable: drop, never block the box
			continue
		}
		if _, err := c.Service.IngestDrafts(toDrafts(plain)); err == nil {
			ingested++
		}
		c.ack(ctx, it.ID)
	}
	return ingested, nil
}

func (c *RelayClient) ack(ctx context.Context, id string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+"/v1/envelopes/"+id+":acknowledge", nil)
	if err != nil {
		return
	}
	resp, err := c.http.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// toDrafts decodes the canonical draft array; undecodable payloads yield an
// empty slice so the envelope is acked and dropped (never blocks the box).
func toDrafts(plain []byte) []ingest.Draft {
	var drafts []ingest.Draft
	if json.Unmarshal(plain, &drafts) != nil {
		return nil
	}
	return drafts
}
