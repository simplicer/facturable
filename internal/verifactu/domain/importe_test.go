package domain

import "testing"

func TestNewImporteNormalizesToTwoDecimals(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "one decimal", raw: "21.4", want: "21.40"},
		{name: "two decimals", raw: "21.40", want: "21.40"},
		{name: "no decimals", raw: "21", want: "21.00"},
		{name: "comma separator", raw: "21,4", want: "21.40"},
		{name: "negative one decimal", raw: "-131.4", want: "-131.40"},
		{name: "negative two decimals", raw: "-131.45", want: "-131.45"},
		{name: "zero", raw: "0", want: "0.00"},
		{name: "edges trimmed", raw: " 12.35 ", want: "12.35"},
		{name: "aeat example cuota", raw: "12.35", want: "12.35"},
		{name: "aeat example importe", raw: "123.45", want: "123.45"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewImporte(tt.raw)
			if err != nil {
				t.Fatalf("NewImporte(%q) unexpected error: %v", tt.raw, err)
			}
			if got.Value() != tt.want {
				t.Fatalf("NewImporte(%q).Value() = %q, want %q", tt.raw, got.Value(), tt.want)
			}
		})
	}
}

func TestImporteWithoutTrailingZeros(t *testing.T) {
	// Official huella spec: numeric values keep "one or two decimal positions,
	// trailing zeros having no relevance" (AEAT huella doc v0.1.2, page 6).
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "aeat vector keeps one decimal", raw: "21.40", want: "21.4"},
		{name: "integer drops the dot", raw: "21.00", want: "21"},
		{name: "two significant decimals unchanged", raw: "12.35", want: "12.35"},
		{name: "negative loses trailing zero", raw: "-131.40", want: "-131.4"},
		{name: "zero collapses", raw: "0.00", want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewImporte(tt.raw)
			if err != nil {
				t.Fatalf("NewImporte(%q) unexpected error: %v", tt.raw, err)
			}
			if gotHash := got.WithoutTrailingZeros(); gotHash != tt.want {
				t.Fatalf("WithoutTrailingZeros() = %q, want %q", gotHash, tt.want)
			}
		})
	}
}

func TestNewImporteInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "only spaces", raw: " "},
		{name: "three decimals", raw: "21.401"},
		{name: "two separators", raw: "12.3.4"},
		{name: "not a number", raw: "doce"},
		{name: "double sign", raw: "--12"},
		{name: "lone dot", raw: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewImporte(tt.raw); err == nil {
				t.Fatalf("NewImporte(%q) expected error, got none", tt.raw)
			}
		})
	}
}

func TestCuotaTotalAndImporteTotalAreImporte(t *testing.T) {
	cuota, err := NewCuotaTotal("21.4")
	if err != nil {
		t.Fatalf("NewCuotaTotal unexpected error: %v", err)
	}
	importe, err := NewImporteTotal("131.4")
	if err != nil {
		t.Fatalf("NewImporteTotal unexpected error: %v", err)
	}
	if cuota.Value() != "21.40" || importe.Value() != "131.40" {
		t.Fatalf("aliases lost normalization: %q / %q", cuota.Value(), importe.Value())
	}
}
