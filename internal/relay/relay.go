// Package relay implements the end-to-end ingestion mailbox: senders drop
// encrypted envelopes (X25519 ECDH + AES-256-GCM, sealed to the agent's
// public key); the agent fetches, decrypts and ingests them. The server
// only stores opaque ciphertext with a TTL — it cannot read invoices.
package relay

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

// Envelope is the wire form uploaded by senders.
type Envelope struct {
	EphemeralPub []byte `json:"ephemeral_pub"`
	Nonce        []byte `json:"nonce"`
	Ciphertext   []byte `json:"ciphertext"`
}

// ErrBadEnvelope marks malformed or undecryptable envelopes.
var ErrBadEnvelope = errors.New("invalid envelope")

// hkdfSHA256 derives a 32-byte key from the shared secret with context
// info + salt (audit: raw sha256(shared) was too weak a KDF).
func hkdfSHA256(shared, salt, info []byte) []byte {
	prk := hmac.New(sha256.New, salt)
	prk.Write(shared)
	okm := make([]byte, 32)
	tm := hmac.New(sha256.New, prk.Sum(nil))
	tm.Write(info)
	tm.Write([]byte{1})
	copy(okm, tm.Sum(nil))
	return okm
}

// relayInfo is the KDF context binding envelopes to this protocol.
var relayInfo = []byte("facturable-relay-v1/x25519-aes256gcm")

// Seal encrypts plaintext to the agent public key (X25519 ECDH, HKDF,
// AES-256-GCM). Senders use it; the server never can.
func Seal(agentPub *ecdh.PublicKey, plaintext []byte) (*Envelope, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(agentPub)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadEnvelope, err)
	}
	key := hkdfSHA256(shared, eph.PublicKey().Bytes(), relayInfo)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return &Envelope{
		EphemeralPub: eph.PublicKey().Bytes(),
		Nonce:        nonce,
		Ciphertext:   gcm.Seal(nil, nonce, plaintext, nil),
	}, nil
}

// Open decrypts an envelope with the agent private key.
func Open(agentPriv *ecdh.PrivateKey, env *Envelope) ([]byte, error) {
	if env == nil || len(env.EphemeralPub) != 32 || len(env.Nonce) < 12 {
		return nil, ErrBadEnvelope
	}
	eph, err := ecdh.X25519().NewPublicKey(env.EphemeralPub)
	if err != nil {
		return nil, ErrBadEnvelope
	}
	shared, err := agentPriv.ECDH(eph)
	if err != nil {
		return nil, ErrBadEnvelope
	}
	key := hkdfSHA256(shared, env.EphemeralPub, relayInfo)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, env.Nonce, env.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: wrong key or damaged payload", ErrBadEnvelope)
	}
	return plain, nil
}

// TTL of stored envelopes (PRODUCTO-MVP §10.4).
const TTL = 7 * 24 * time.Hour
