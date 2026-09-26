package domain

import "testing"

func TestNewTipoFacturaValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want TipoFactura
	}{
		{name: "factura completa", raw: "F1", want: FacturaCompleta},
		{name: "factura simplificada", raw: "F2", want: FacturaSimplificada},
		{name: "factura sustitutiva", raw: "F3", want: FacturaSustitutiva},
		{name: "factura por error fundado", raw: "F4", want: FacturaPorErrorFundado},
		{name: "rectificativa 1", raw: "R1", want: Rectificativa1},
		{name: "rectificativa 2", raw: "R2", want: Rectificativa2},
		{name: "rectificativa 3", raw: "R3", want: Rectificativa3},
		{name: "rectificativa 4", raw: "R4", want: Rectificativa4},
		{name: "rectificativa 5", raw: "R5", want: Rectificativa5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewTipoFactura(tt.raw)
			if err != nil {
				t.Fatalf("NewTipoFactura(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("NewTipoFactura(%q) = %q, want %q", tt.raw, got, tt.want)
			}
			if got.String() != tt.raw {
				t.Fatalf("String() = %q, want %q", got.String(), tt.raw)
			}
		})
	}
}

func TestNewTipoFacturaInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "unknown code", raw: "F5"},
		{name: "rectificativa out of range", raw: "R0"},
		{name: "rectificativa beyond range", raw: "R6"},
		{name: "lowercase rejected", raw: "f1"},
		{name: "empty", raw: ""},
		{name: "not a code", raw: "factura"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTipoFactura(tt.raw); err == nil {
				t.Fatalf("NewTipoFactura(%q) expected error, got none", tt.raw)
			}
		})
	}
}
