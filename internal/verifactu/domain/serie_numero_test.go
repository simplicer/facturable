package domain

import (
	"strings"
	"testing"
)

func TestNewSerieNumeroValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "plain number", raw: "1234", want: "1234"},
		{name: "number with series", raw: "12345678/G33", want: "12345678/G33"},
		{name: "inner spaces preserved", raw: "12345678 / G33", want: "12345678 / G33"},
		{name: "edges trimmed", raw: "  A-001  ", want: "A-001"},
		{name: "single char", raw: "1", want: "1"},
		{name: "sixty chars accepted", raw: strings.Repeat("A", 60), want: strings.Repeat("A", 60)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSerieNumero(tt.raw)
			if err != nil {
				t.Fatalf("NewSerieNumero(%q) unexpected error: %v", tt.raw, err)
			}
			if got.Value() != tt.want {
				t.Fatalf("NewSerieNumero(%q).Value() = %q, want %q", tt.raw, got.Value(), tt.want)
			}
		})
	}
}

func TestNewSerieNumeroInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "only spaces", raw: "   "},
		{name: "sixty one chars rejected", raw: strings.Repeat("A", 61)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSerieNumero(tt.raw); err == nil {
				t.Fatalf("NewSerieNumero(%q) expected error, got none", tt.raw)
			}
		})
	}
}
