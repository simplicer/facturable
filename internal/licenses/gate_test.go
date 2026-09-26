package licenses

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// expiry used by gate tests: 2027-01-31T23:59:59Z
var gateExpiry = time.Date(2027, 1, 31, 23, 59, 59, 0, time.UTC)

func gateLicense(grace int) License {
	return NewLicense("0195c4d2-7f6a-7b3e-9c1d-2a4b6c8d0e1f", PlanBasico,
		[]string{"B12345678"},
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), gateExpiry, grace)
}

func TestGateStampNewFollowsGraceWindow(t *testing.T) {
	t.Parallel()

	day := 24 * time.Hour
	tests := []struct {
		name    string
		grace   int
		now     time.Time
		wantErr bool
	}{
		{name: "long before expiry", grace: 7, now: gateExpiry.Add(-90 * day), wantErr: false},
		{name: "last second of expiry day", grace: 7, now: gateExpiry, wantErr: false},
		{name: "one day into grace", grace: 7, now: gateExpiry.Add(1 * day), wantErr: false},
		{name: "six days into grace", grace: 7, now: gateExpiry.Add(6 * day), wantErr: false},
		{name: "last day of grace", grace: 7, now: gateExpiry.Add(7 * day), wantErr: false},
		{name: "one nanosecond past grace end", grace: 7, now: gateExpiry.Add(7*day + time.Nanosecond), wantErr: true},
		{name: "eight days past expiry", grace: 7, now: gateExpiry.Add(8 * day), wantErr: true},
		{name: "one year past expiry", grace: 7, now: gateExpiry.Add(365 * day), wantErr: true},
		{name: "zero grace blocks right after expiry", grace: 0, now: gateExpiry.Add(time.Nanosecond), wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := gateLicense(tt.grace)
			if tt.grace == 0 {
				// NewLicense maps 0 to the default; a literal license with
				// GraceDays 0 must mean "no grace at all".
				l.GraceDays = 0
			}

			err := Gate(l, tt.now, ActionStampNew)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Gate(stamp) at %v = %v, want allowed", tt.now, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Gate(stamp) at %v = nil, want blocked", tt.now)
			}
			if !errors.Is(err, ErrLicenseExpired) {
				t.Fatalf("Gate(stamp) error = %v, want wrapping ErrLicenseExpired", err)
			}
			if !strings.Contains(err.Error(), "abrir") || !strings.Contains(err.Error(), "exportar") {
				t.Fatalf("Gate(stamp) error = %q, must tell the user that opening and exporting stay available", err)
			}
		})
	}
}

func TestGateNeverHostage(t *testing.T) {
	t.Parallel()

	// License long expired, far beyond any grace: opening data and exporting
	// MUST still work (never hostage), only stamping new invoices is blocked.
	l := gateLicense(7)
	longAfter := gateExpiry.Add(400 * 24 * time.Hour)

	for _, action := range []Action{ActionOpenData, ActionExport} {
		if err := Gate(l, longAfter, action); err != nil {
			t.Fatalf("Gate(%v) long after expiry = %v, want always allowed", action, err)
		}
	}
	if err := Gate(l, longAfter, ActionStampNew); !errors.Is(err, ErrLicenseExpired) {
		t.Fatalf("Gate(stamp) long after expiry = %v, want wrapping ErrLicenseExpired", err)
	}
}

func TestGateUnknownActionIsDenied(t *testing.T) {
	t.Parallel()

	l := gateLicense(7)
	err := Gate(l, gateExpiry.Add(-24*time.Hour), Action("borrarTodo"))
	if err == nil {
		t.Fatal("Gate(unknown action) = nil, want denied")
	}
	if !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("Gate(unknown action) error = %v, want wrapping ErrUnknownAction", err)
	}
}

func TestGateAllActionsInsideValidity(t *testing.T) {
	t.Parallel()

	l := gateLicense(7)
	now := gateExpiry.Add(-24 * time.Hour)
	for _, action := range []Action{ActionStampNew, ActionOpenData, ActionExport} {
		if err := Gate(l, now, action); err != nil {
			t.Fatalf("Gate(%v) while valid = %v, want allowed", action, err)
		}
	}
}
