package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewFechaHoraHusoValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "spain peninsula offset", raw: "2024-11-04T12:36:39+01:00", want: "2024-11-04T12:36:39+01:00"},
		{name: "canary offset", raw: "2024-11-04T11:36:39+00:00", want: "2024-11-04T11:36:39+00:00"},
		{name: "utc marker", raw: "2024-01-01T19:20:30Z", want: "2024-01-01T19:20:30Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFechaHoraHuso(tt.raw)
			if err != nil {
				t.Fatalf("NewFechaHoraHuso(%q) unexpected error: %v", tt.raw, err)
			}
			if got.ISO8601() != tt.want {
				t.Fatalf("ISO8601() = %q, want %q", got.ISO8601(), tt.want)
			}
		})
	}
}

func TestNewFechaHoraHusoInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing timezone offset", raw: "2024-11-04T12:36:39"},
		{name: "plain date", raw: "04-11-2024"},
		{name: "empty", raw: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewFechaHoraHuso(tt.raw); err == nil {
				t.Fatalf("NewFechaHoraHuso(%q) expected error, got none", tt.raw)
			}
		})
	}
}

func TestNewFechaHoraHusoFromTimeKeepsOffset(t *testing.T) {
	stamp := parseTimeForTest(t, "2024-11-04T12:36:39+01:00")
	got := NewFechaHoraHusoFromTime(stamp)
	if got.ISO8601() != "2024-11-04T12:36:39+01:00" {
		t.Fatalf("ISO8601() = %q, want the original offset preserved", got.ISO8601())
	}
}

// parseTimeForTest is the only clock-shaped helper: tests seed explicit
// instants so the domain never reads the wall clock.
func parseTimeForTest(t *testing.T, raw string) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("time.Parse(%q) unexpected error: %v", raw, err)
	}
	return value
}

func TestFromTimeUTCProduceOffsetNumerico(t *testing.T) {
	f := NewFechaHoraHusoFromTime(time.Date(2027, 1, 5, 10, 0, 0, 0, time.UTC))
	raw := f.ISO8601()
	if strings.HasSuffix(raw, "Z") {
		t.Fatalf("la AEAT no acepta Z: %s", raw)
	}
	if !strings.HasSuffix(raw, "+01:00") {
		t.Fatalf("invierno debe ser +01:00: %s", raw)
	}
}
