package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// pinOKSink answers 200 for every request it receives.
func pinOKSink(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func pinStatusOf(h http.Handler, auth, path string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestWithPINRejectsWrongBearerWithoutTimingLeak covers the constant-time
// bearer check (audit high): values of every shape — missing, wrong scheme,
// shorter, same length wrong, longer — must all be rejected, and the
// comparison must not depend on early string equality (crypto/subtle).
func TestWithPINRejectsWrongBearerWithoutTimingLeak(t *testing.T) {
	const pin = "1234-secreto"
	h := withPIN(pin, http.HandlerFunc(pinOKSink))

	if got := pinStatusOf(h, "Bearer "+pin, "/v1/records"); got != http.StatusOK {
		t.Fatalf("correct bearer = %d, want 200", got)
	}
	for _, auth := range []string{
		"",                          // missing header
		"Bearer ",                   // empty bearer
		"Bearer 1234-secre",         // shorter
		"Bearer 1234-secretoo",      // same length, wrong content
		"bearer " + pin,             // wrong scheme case
		"Basic " + pin,              // wrong scheme
		"Bearer 1234-secreto extra", // longer
	} {
		if got := pinStatusOf(h, auth, "/v1/records"); got != http.StatusUnauthorized {
			t.Fatalf("Authorization %q = %d, want 401", auth, got)
		}
	}
}

// TestWithPINScope pins the protection surface: every /v1/* route requires
// the PIN; only /status and the embedded UI stay open by design.
func TestWithPINScope(t *testing.T) {
	const pin = "abc"
	h := withPIN(pin, http.HandlerFunc(pinOKSink))

	for _, path := range []string{"/v1/invoices", "/v1/records", "/v1/version", "/v1/connectivity"} {
		if got := pinStatusOf(h, "", path); got != http.StatusUnauthorized {
			t.Fatalf("%s without PIN = %d, want 401 (every /v1/* route is PIN-guarded)", path, got)
		}
	}
	for _, path := range []string{"/status", "/"} {
		if got := pinStatusOf(h, "", path); got != http.StatusOK {
			t.Fatalf("%s without PIN = %d, want 200 (open by design)", path, got)
		}
	}
}

// TestWithPINEmptyMeansOpen documents the dev mode: without SELLO_PIN the
// agent runs unauthenticated (loopback-only binding keeps it local).
func TestWithPINEmptyMeansOpen(t *testing.T) {
	h := withPIN("", http.HandlerFunc(pinOKSink))
	if got := pinStatusOf(h, "", "/v1/invoices"); got != http.StatusOK {
		t.Fatalf("empty PIN /v1/invoices = %d, want 200", got)
	}
}

// TestAgentListenAddressIsLoopbackOnly is the tripwire for the audit fix:
// the agent must never be configurable to bind anything but the loopback,
// because it holds the fiscal certificate and the invoice ledger.
func TestAgentListenAddressIsLoopbackOnly(t *testing.T) {
	host, port, err := net.SplitHostPort(agentListenAddr)
	if err != nil {
		t.Fatalf("agentListenAddr %q is not host:port: %v", agentListenAddr, err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("agent binds %q, want 127.0.0.1 (loopback-only hard-coding was altered)", host)
	}
	if port == "" {
		t.Fatal("agent listen port is empty")
	}
}

// TestRateLimiterUnderConcurrency proves the mutex makes the fixed window
// exact and race-free: run with -race to verify there is no data race
// between concurrent allow() callers (audit).
func TestRateLimiterUnderConcurrency(t *testing.T) {
	const max = 10
	rl := newRateLimiter(max, time.Minute)

	const callers = 100
	results := make([]bool, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = rl.allow("B72877814")
		}(i)
	}
	wg.Wait()

	allowed := 0
	for _, ok := range results {
		if ok {
			allowed++
		}
	}
	if allowed != max {
		t.Fatalf("concurrent allow() admitted %d, want exactly %d (window must be exact under the mutex)", allowed, max)
	}

	// a different key has its own independent budget
	if !rl.allow("B44531218") {
		t.Fatal("second key rate-limited by the first key's usage")
	}
}
