package relay

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	agent, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secreto := []byte(`[{"numero_serie":"RELAY-0001","fecha":"26-09-2026","tipo":"F1","descripcion":"via relay","nif_cliente":"B44531218","nombre_cliente":"ACME","desgloses":[{"tipo_impositivo":"21","base":"10.00","cuota":"2.10"}]}]`)

	env, err := Seal(agent.PublicKey(), secreto)
	if err != nil {
		t.Fatalf("Seal(): %v", err)
	}
	got, err := Open(agent, env)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	if !bytes.Equal(got, secreto) {
		t.Fatal("round-trip mismatch")
	}

	// clave equivocada no abre
	otra, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if _, err := Open(otra, env); err == nil {
		t.Fatal("envelope abierta con clave ajena")
	}
	// basura no abre
	if _, err := Open(agent, &Envelope{EphemeralPub: env.EphemeralPub, Nonce: env.Nonce, Ciphertext: []byte("xxx")}); err == nil {
		t.Fatal("ciphertext manipulado aceptado")
	}
}
