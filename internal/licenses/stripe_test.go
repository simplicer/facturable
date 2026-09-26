package licenses

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// firmar construye una header Stripe-Signature válida.
func firmar(secret, payload string, t time.Time) string {
	ts := fmt.Sprintf("%d", t.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", ts, payload)
	return fmt.Sprintf("t=%s,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func postear(t *testing.T, srv *httptest.Server, cuerpo, firma string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/stripe-webhook", bytes.NewReader([]byte(cuerpo)))
	req.Header.Set("Stripe-Signature", firma)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestStripeWebhookEmiteLicenciaIdempotente(t *testing.T) {
	api, srv := nuevoAPIPrueba(t, time.Now)
	mux := http.NewServeMux()
	api.Mount(mux)
	api.MountStripe(mux, "whsec_test")
	ws := httptest.NewServer(mux)
	t.Cleanup(ws.Close)

	evento := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{
		"client_reference_id":"66666666-6666-6666-6666-666666666666",
		"metadata":{"plan":"pro","nifs":"B72877814","dias_validez":"31","grace_days":"7"}}}}`
	ahora := time.Now()

	r := postear(t, ws, evento, firmar("whsec_test", evento, ahora))
	if r.StatusCode != 201 {
		t.Fatalf("primer webhook = %d", r.StatusCode)
	}
	// replay mismo evento → 200 no-op
	r = postear(t, ws, evento, firmar("whsec_test", evento, ahora))
	if r.StatusCode != 200 {
		t.Fatalf("replay = %d, want 200", r.StatusCode)
	}
	// firma inválida → 400
	r = postear(t, ws, evento, "t=1,v1=deadbeef")
	if r.StatusCode != 400 {
		t.Fatalf("firma mala = %d, want 400", r.StatusCode)
	}
	// evento irrelevante → 200 ignored
	r = postear(t, ws, `{"id":"evt_2","type":"customer.updated","data":{"object":{}}}`,
		firmar("whsec_test", `{"id":"evt_2","type":"customer.updated","data":{"object":{}}}`, ahora))
	if r.StatusCode != 200 {
		t.Fatalf("evento ignorado = %d", r.StatusCode)
	}
	// la licencia existe y es válida
	resp, _ := srv.Client().Get(srv.URL + "/v1/licenses/66666666-6666-6666-6666-666666666666/validity")
	if resp.StatusCode != 200 {
		t.Fatalf("validity tras pago = %d", resp.StatusCode)
	}
}

func TestVerifyStripeSignatureReplayViejo(t *testing.T) {
	ahora := time.Now()
	payload := `{}`
	if VerifyStripeSignature("s", payload, firmar("s", payload, ahora.Add(-10*time.Minute)), ahora) {
		t.Fatal("firma con 10 min de antigüedad aceptada (tolerancia 5 min)")
	}
	if !VerifyStripeSignature("s", payload, firmar("s", payload, ahora), ahora) {
		t.Fatal("firma válida rechazada")
	}
	if VerifyStripeSignature("otra", payload, firmar("s", payload, ahora), ahora) {
		t.Fatal("secreto equivocado aceptado")
	}
}
