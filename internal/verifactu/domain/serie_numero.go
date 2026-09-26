package domain

import (
	"fmt"
	"strings"
)

// maxSerieNumeroLength is the AEAT limit for NumSerieFactura (Alfanumérico
// (60), Anexo I del diseño de chainRow de alta).
const maxSerieNumeroLength = 60

// SerieNumero is the invoice number (and optional series) assigned by the
// issuer inside a SIF, unique per issuer and year.
type SerieNumero struct {
	value string
}

// NewSerieNumero validates the length (1..60) and trims surrounding spaces.
// Inner spaces are part of the number (e.g. "12345678 / G33").
func NewSerieNumero(raw string) (SerieNumero, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return SerieNumero{}, fmt.Errorf("el número de factura (número y serie) no puede estar vacío")
	}
	if len(value) > maxSerieNumeroLength {
		return SerieNumero{}, fmt.Errorf(
			"el número de factura %q supera el máximo de %d caracteres", value, maxSerieNumeroLength)
	}
	return SerieNumero{value: value}, nil
}

// Value returns the invoice number as issued.
func (s SerieNumero) Value() string {
	return s.value
}

// IsZero reports whether the number was never built.
func (s SerieNumero) IsZero() bool {
	return s.value == ""
}

func (s SerieNumero) String() string {
	return s.value
}
