package domain

import "testing"

func TestNewFechaExpedicionValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "day-month-year AEAT format", raw: "04-11-2024", want: "04-11-2024"},
		{name: "ISO input normalizes to AEAT format", raw: "2024-11-04", want: "04-11-2024"},
		{name: "first of january", raw: "01-01-2024", want: "01-01-2024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFechaExpedicion(tt.raw)
			if err != nil {
				t.Fatalf("NewFechaExpedicion(%q) unexpected error: %v", tt.raw, err)
			}
			if got.Date() != tt.want {
				t.Fatalf("NewFechaExpedicion(%q).Date() = %q, want %q", tt.raw, got.Date(), tt.want)
			}
		})
	}
}

func TestNewFechaExpedicionInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "impossible day", raw: "31-02-2024"},
		{name: "impossible month", raw: "01-13-2024"},
		{name: "wrong separator", raw: "04/11/2024"},
		{name: "missing year", raw: "04-11"},
		{name: "not a date", raw: "ayer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewFechaExpedicion(tt.raw); err == nil {
				t.Fatalf("NewFechaExpedicion(%q) expected error, got none", tt.raw)
			}
		})
	}
}

func TestNewFechaExpedicionFromTimeKeepsDatePart(t *testing.T) {
	// Seeded time, never the wall clock: the domain stays pure.
	stamp := parseTimeForTest(t, "2024-11-04T23:59:59+01:00")
	got := NewFechaExpedicionFromTime(stamp)
	if got.Date() != "04-11-2024" {
		t.Fatalf("Date() = %q, want 04-11-2024 (date part in the record timezone)", got.Date())
	}
}
