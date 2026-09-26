package licencias

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func testLicense(t *testing.T) License {
	t.Helper()
	return NewLicense("0195c4d2-7f6a-7b3e-9c1d-2a4b6c8d0e1f", PlanPro,
		[]string{"B12345678", "12345678Z"},
		time.Date(2026, 12, 31, 10, 30, 0, 0, time.UTC),
		time.Date(2027, 1, 31, 23, 59, 59, 0, time.UTC), 0)
}

func TestSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()

	pub, priv := testKeys(t)
	l := testLicense(t)

	token, err := Sign(priv, l)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if token == "" {
		t.Fatal("Sign returned empty token")
	}
	if got := strings.Count(token, "."); got != 2 {
		t.Fatalf("token has %d separators, want 2 (header.payload.signature): %q", got, token)
	}

	got, err := Verify(pub, token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.LicenseID != l.LicenseID {
		t.Fatalf("LicenseID = %q, want %q", got.LicenseID, l.LicenseID)
	}
	if got.Plan != l.Plan {
		t.Fatalf("Plan = %q, want %q", got.Plan, l.Plan)
	}
	if strings.Join(got.NIFs, ",") != strings.Join(l.NIFs, ",") {
		t.Fatalf("NIFs = %v, want %v", got.NIFs, l.NIFs)
	}
	if !got.ExpiresAt.Equal(l.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, l.ExpiresAt)
	}
	if !got.IssuedAt.Equal(l.IssuedAt) {
		t.Fatalf("IssuedAt = %v, want %v", got.IssuedAt, l.IssuedAt)
	}
	if got.GraceDays != DefaultGraceDays {
		t.Fatalf("GraceDays = %d, want %d (default)", got.GraceDays, DefaultGraceDays)
	}
}

func TestSignIsDeterministic(t *testing.T) {
	t.Parallel()

	_, priv := testKeys(t)
	l := testLicense(t)

	first, err := Sign(priv, l)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	second, err := Sign(priv, l)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if first != second {
		t.Fatalf("Sign is not deterministic for identical input:\n%q\n%q", first, second)
	}
}

func TestSignRejectsBadInput(t *testing.T) {
	t.Parallel()

	_, priv := testKeys(t)

	tests := []struct {
		name    string
		key     ed25519.PrivateKey
		license License
		wantErr string
	}{
		{
			name:    "wrong private key size",
			key:     ed25519.PrivateKey(make([]byte, 10)),
			license: testLicense(t),
			wantErr: "clave privada",
		},
		{
			name:    "unknown plan",
			key:     priv,
			license: License{LicenseID: "lic-1", Plan: Plan("ultra"), ExpiresAt: time.Now().Add(time.Hour), IssuedAt: time.Now(), GraceDays: 7},
			wantErr: "plan",
		},
		{
			name:    "missing identifier",
			key:     priv,
			license: License{Plan: PlanPro, ExpiresAt: time.Now().Add(time.Hour), IssuedAt: time.Now(), GraceDays: 7},
			wantErr: "identificador",
		},
		{
			name:    "missing expiry",
			key:     priv,
			license: License{LicenseID: "lic-1", Plan: PlanPro, IssuedAt: time.Now(), GraceDays: 7},
			wantErr: "caducidad",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			token, err := Sign(tt.key, tt.license)
			if err == nil {
				t.Fatalf("Sign() = %q, want error containing %q", token, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Sign() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	t.Parallel()

	pub, priv := testKeys(t)
	l := testLicense(t)
	token, err := Sign(priv, l)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	parts := strings.Split(token, ".")

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "not enough parts", token: parts[0] + "." + parts[1]},
		{name: "extra part", token: token + "." + parts[0]},
		{name: "tampered payload", token: parts[0] + ".eyJsaWNlbnNlX2lkIjoiZXZpbCJ9." + parts[2]},
		{name: "tampered header", token: "eyJhbGciOiJub25lIn0." + parts[1] + "." + parts[2]},
		{name: "tampered signature", token: parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2]))},
		{name: "payload replaced with other valid license payload", token: parts[0] + "." + tamperedPayloadB64(t, priv, l) + "." + parts[2]},
		{name: "garbage", token: "not-a-token"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Verify(pub, tt.token)
			if err == nil {
				t.Fatalf("Verify() = %+v, want error", got)
			}
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify() error = %v, want wrapping ErrInvalidToken", err)
			}
		})
	}
}

// tamperedPayloadB64 builds a base64url payload claiming a different license ID,
// signed with the same key: it must still fail because the signature no longer
// matches header+payload.
func tamperedPayloadB64(t *testing.T, priv ed25519.PrivateKey, l License) string {
	t.Helper()
	evil := l
	evil.LicenseID = "0195c4d2-0000-7b3e-9c1d-000000000000"
	b, err := marshalPayload(evil)
	if err != nil {
		t.Fatalf("marshalPayload: %v", err)
	}
	return base64URL(b)
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	t.Parallel()

	pubA, privA := testKeys(t)
	pubB, privB := testKeys(t)

	token, err := Sign(privA, testLicense(t))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := Verify(pubA, token); err != nil {
		t.Fatalf("Verify with signing key: %v", err)
	}

	// Verify with a different public key (swap one byte) must fail.
	otherPub := append(ed25519.PublicKey(nil), pubA...)
	otherPub[0] ^= 0xFF
	if _, err := Verify(otherPub, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify with wrong key error = %v, want wrapping ErrInvalidToken", err)
	}
	if _, err := Verify(ed25519.PublicKey(make([]byte, 3)), token); err == nil {
		t.Fatal("Verify with malformed key = nil error, want error")
	}

	// A token signed by another vendor key must not verify against pubA.
	tokenB, err := Sign(privB, testLicense(t))
	if err != nil {
		t.Fatalf("Sign with key B: %v", err)
	}
	if _, err := Verify(pubA, tokenB); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(key-B token) with key A error = %v, want wrapping ErrInvalidToken", err)
	}
	if _, err := Verify(pubB, tokenB); err != nil {
		t.Fatalf("Verify(key-B token) with key B: %v", err)
	}
}

func TestVerifyRejectsStructurallyInvalidLicense(t *testing.T) {
	t.Parallel()

	// A token signed by a real key but whose payload encodes an invalid license
	// (unknown plan) must be rejected even though the signature is valid.
	pub, priv := testKeys(t)
	invalid := License{
		LicenseID: "lic-1",
		Plan:      Plan("gratis"),
		ExpiresAt: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC),
		IssuedAt:  time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		GraceDays: 7,
	}
	payload, err := marshalPayload(invalid)
	if err != nil {
		t.Fatalf("marshalPayload: %v", err)
	}
	signingInput := encodedHeader() + "." + base64URL(payload)
	sig := ed25519.Sign(priv, []byte(signingInput))
	token := signingInput + "." + base64URL(sig)

	if _, err := Verify(pub, token); err == nil {
		t.Fatal("Verify() = nil error, want error for invalid plan inside valid signature")
	}
}
