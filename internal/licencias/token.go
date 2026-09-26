package licencias

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken is returned when a license token cannot be trusted:
// malformed encoding, bad signature or incoherent content.
var ErrInvalidToken = errors.New("licencia no válida")

// tokenHeader is the fixed JOSE-style header of license tokens.
type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

const (
	tokenAlg = "EdDSA"
	tokenTyp = "license+json"
)

// licensePayload is the JSON wire form of a License.
type licensePayload struct {
	LicenseID string    `json:"license_id"`
	Plan      Plan      `json:"plan"`
	NIFs      []string  `json:"nifs,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	GraceDays int       `json:"grace_days"`
	IssuedAt  time.Time `json:"issued_at"`
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func encodedHeader() string {
	b, _ := json.Marshal(tokenHeader{Alg: tokenAlg, Typ: tokenTyp})
	return base64URL(b)
}

func marshalPayload(l License) ([]byte, error) {
	return json.Marshal(licensePayload{
		LicenseID: l.LicenseID,
		Plan:      l.Plan,
		NIFs:      l.NIFs,
		ExpiresAt: l.ExpiresAt,
		GraceDays: l.GraceDays,
		IssuedAt:  l.IssuedAt,
	})
}

// Sign returns a compact signed token for the license: a base64url JSON
// payload with an Ed25519 signature over header.payload (JWT-style shape,
// no third-party dependencies). Signing is deterministic.
func Sign(priv ed25519.PrivateKey, l License) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("%w: clave privada ed25519 no válida", ErrInvalidToken)
	}
	if l.GraceDays == 0 {
		l.GraceDays = DefaultGraceDays
	}
	if err := l.Validate(); err != nil {
		return "", err
	}

	payload, err := marshalPayload(l)
	if err != nil {
		return "", fmt.Errorf("%w: no se pudo serializar la licencia: %v", ErrInvalidToken, err)
	}

	signingInput := encodedHeader() + "." + base64URL(payload)
	sig := ed25519.Sign(priv, []byte(signingInput))
	return signingInput + "." + base64URL(sig), nil
}

// Verify checks the token signature with the vendor public key and returns
// the embedded License. It does not decide whether the license is usable:
// expiry and the offline grace period are business rules applied by Gate
// with an injected time, so verification works fully offline.
func Verify(pub ed25519.PublicKey, token string) (License, error) {
	if len(pub) != ed25519.PublicKeySize {
		return License{}, fmt.Errorf("%w: clave pública ed25519 no válida", ErrInvalidToken)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return License{}, fmt.Errorf("%w: formato de token incorrecto", ErrInvalidToken)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return License{}, fmt.Errorf("%w: el cuerpo del token no está en base64url", ErrInvalidToken)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return License{}, fmt.Errorf("%w: la firma del token no está en base64url", ErrInvalidToken)
	}

	signingInput := parts[0] + "." + parts[1]
	if !ed25519.Verify(pub, []byte(signingInput), sig) {
		return License{}, fmt.Errorf("%w: la firma del token no coincide", ErrInvalidToken)
	}

	var head tokenHeader
	rawHead, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(rawHead, &head) != nil {
		return License{}, fmt.Errorf("%w: cabecera de token ilegible", ErrInvalidToken)
	}
	if head.Alg != tokenAlg || head.Typ != tokenTyp {
		return License{}, fmt.Errorf("%w: token con algoritmo o tipo no soportados (%s/%s)", ErrInvalidToken, head.Alg, head.Typ)
	}

	var p licensePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return License{}, fmt.Errorf("%w: el cuerpo del token no es una licencia", ErrInvalidToken)
	}
	l := License{
		LicenseID: p.LicenseID,
		Plan:      p.Plan,
		NIFs:      p.NIFs,
		ExpiresAt: p.ExpiresAt,
		GraceDays: p.GraceDays,
		IssuedAt:  p.IssuedAt,
	}
	if l.GraceDays == 0 {
		l.GraceDays = DefaultGraceDays
	}
	if err := l.Validate(); err != nil {
		return License{}, err
	}
	return l, nil
}
