package licenses

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Stripe webhook handling (stdlib only): signature verification per Stripe
// docs (HMAC-SHA256 over "{t}.{payload}") and idempotent license issuance
// on completed checkout sessions. Stripe-go is not required for this path
// (ADR-003: stdlib first).

// VerifyStripeSignature checks the Stripe-Signature header against the
// payload with a 5-minute replay tolerance.
func VerifyStripeSignature(secret, payload, header string, now time.Time) bool {
	var ts string
	macs := make([][]byte, 0, 1)
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			raw, err := hex.DecodeString(v)
			if err == nil {
				macs = append(macs, raw)
			}
		}
	}
	if ts == "" || len(macs) == 0 {
		return false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(sec, 0)); d > 5*time.Minute || d < -5*time.Minute {
		return false // replay outside tolerance, both directions (audit)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%s", ts, payload)
	want := mac.Sum(nil)
	for _, m := range macs {
		if hmac.Equal(m, want) {
			return true
		}
	}
	return false
}

// stripeEvent is the minimal event surface this service consumes.
type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ClientReferenceID string            `json:"client_reference_id"`
			Metadata          map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

// MountStripe registers POST /v1/stripe-webhook. A valid completed
// checkout session with client_reference_id = license UUID issues the
// license described in its metadata (plan, nifs CSV, days). Replays are
// idempotent no-ops.
func (a *API) MountStripe(mux *http.ServeMux, secret string) {
	mux.HandleFunc("POST /v1/stripe:webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			problem(w, 400, "unreadable body", r)
			return
		}
		if !VerifyStripeSignature(secret, string(body), r.Header.Get("Stripe-Signature"), a.ahora()) {
			// 401 sin detalle: evita fingerprinting y no incentiva reintentos
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Unauthorized", "status": 401})
			return
		}
		var ev stripeEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			problem(w, 400, "invalid event json", r)
			return
		}
		switch ev.Type {
		case "checkout.session.completed", "invoice.payment_succeeded":
			// fall through to issuance
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ignored":true}`))
			return
		}
		id := ev.Data.Object.ClientReferenceID
		md := ev.Data.Object.Metadata
		if !isUUID(id) || md["plan"] == "" {
			problem(w, 422, "event lacks client_reference_id (uuid) or plan metadata", r)
			return
		}
		var nifs []string
		if md["nifs"] != "" {
			nifs = strings.Split(md["nifs"], ",")
		}
		days, _ := strconv.Atoi(md["dias_validez"])
		if days <= 0 {
			days = 31
		}
		lic, _, err := a.issue(id, Plan(md["plan"]), nifs, days, md["grace_days"])
		if errors.Is(err, errLicenseExists) {
			w.WriteHeader(http.StatusOK) // replay: already issued
			_, _ = w.Write([]byte(`{"replayed":true}`))
			return
		}
		if err != nil {
			problem(w, 422, err.Error(), r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"license_id": lic.LicenseID}})
	})
}

var errLicenseExists = errors.New("license already exists")

// issue is the shared creation path (also used by PUT); it does NOT
// conflict on existing data like PUT does: webhook replays are no-ops.
func (a *API) issue(id string, plan Plan, nifs []string, days int, grace string) (License, string, error) {
	graceDays, _ := strconv.Atoi(grace)
	ahora := a.ahora().UTC()
	lic := NewLicense(id, plan, nifs, ahora, ahora.AddDate(0, 0, days), graceDays)
	var existing string
	if err := a.store.db.QueryRow(`SELECT token FROM licencias WHERE id=?;`, id).Scan(&existing); err == nil {
		return License{}, "", errLicenseExists
	}
	token, err := Sign(a.priv, lic)
	if err != nil {
		return License{}, "", err
	}
	_, err = a.store.db.Exec(`INSERT INTO licencias (id, plan, nifs, expires_at, grace_days, issued_at, token, body)
VALUES (?,?,?,?,?,?,?,?);`, id, string(plan), marshalNIFs(nifs),
		lic.ExpiresAt.Format(time.RFC3339), lic.GraceDays, lic.IssuedAt.Format(time.RFC3339), token,
		fmt.Sprintf("stripe|%s|%d|%d", plan, days, graceDays))
	if err != nil {
		return License{}, "", err
	}
	return lic, token, nil
}
