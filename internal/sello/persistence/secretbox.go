package persistence

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

// EncryptedFile stores a secret (e.g. the customer P12) encrypted at rest
// with a key derived from the operator PIN (PBKDF2-SHA256, AES-256-GCM).
// Nothing readable touches the disk (audit high: P12 in clear 0600).
type EncryptedFile struct{ magic, salt, nonce, body []byte }

var secretMagic = []byte("FSB1")

// WriteSecret encrypts plaintext into path.
func WriteSecret(path string, plaintext []byte, pin string) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := pbkdf2.Key([]byte(pin), salt, 210_000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := append([]byte{}, secretMagic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, gcm.Seal(nil, nonce, plaintext, nil)...)
	return os.WriteFile(path, out, 0o600)
}

// ReadSecret decrypts a file written by WriteSecret.
func ReadSecret(path string, pin string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < len(secretMagic)+16+12 || string(raw[:4]) != string(secretMagic) {
		return nil, errors.New("fichero de secreto no reconocido")
	}
	body := raw[4:]
	salt, rest := body[:16], body[16:]
	nonce, sealed := rest[:12], rest[12:]
	key := pbkdf2.Key([]byte(pin), salt, 210_000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf("PIN incorrecto o secreto dañado: %v", err)
	}
	return plain, nil
}
