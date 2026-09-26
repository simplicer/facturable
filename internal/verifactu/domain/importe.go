package domain

import (
	"fmt"
	"strings"
)

// decimalImporteLayout pieces of the canonical representation: exact two
// decimal positions, dot separator, optional sign, never a float (repo rule:
// "Numeric amounts: exact 2 decimals as strings in domain").
const (
	importeDigits = "0123456789"
	importeDot    = "."
)

// Importe is a monetary amount carried as an exact decimal string with
// exactly two decimal positions. Floats are forbidden in the domain.
type Importe struct {
	value string // canonical form: optional '-', digits, '.', exactly 2 digits.
}

// CuotaTotal is the sum of tax quotas of an invoice.
type CuotaTotal = Importe

// ImporteTotal is the total amount of an invoice (base amounts plus quotas).
type ImporteTotal = Importe

// NewCuotaTotal builds the total quota amount.
func NewCuotaTotal(raw string) (CuotaTotal, error) {
	return NewImporte(raw)
}

// NewImporteTotal builds the total invoice amount.
func NewImporteTotal(raw string) (ImporteTotal, error) {
	return NewImporte(raw)
}

// NewImporte parses a user-supplied amount ("21", "21.4", "21,40", "-131.4")
// and normalizes it to the canonical two-decimal string.
func NewImporte(raw string) (Importe, error) {
	trimmed := trimSpaces(raw)
	if trimmed == "" {
		return Importe{}, fmt.Errorf("el importe no puede estar vacío")
	}
	sign := ""
	body := trimmed
	if body[0] == '-' {
		sign, body = "-", body[1:]
	}
	body = strings.ReplaceAll(body, ",", importeDot)

	intPart, decPart, found := strings.Cut(body, importeDot)
	if !allDigits(intPart) || intPart == "" {
		return Importe{}, fmt.Errorf("el importe %q no es válido (ejemplos válidos: 21, 21.4, -131.45)", raw)
	}
	if found && !allDigits(decPart) {
		return Importe{}, fmt.Errorf("el importe %q no es válido (ejemplos válidos: 21, 21.4, -131.45)", raw)
	}
	switch {
	case !found:
		decPart = "00"
	case len(decPart) == 1:
		decPart += "0"
	case len(decPart) > 2:
		return Importe{}, fmt.Errorf(
			"el importe %q no es válido: el dominio trabaja con exactamente 2 decimales", raw)
	}
	return Importe{value: sign + intPart + importeDot + decPart}, nil
}

// Value returns the canonical amount: sign, integer part and exactly two
// decimal positions with a dot separator.
func (i Importe) Value() string {
	return i.value
}

// WithoutTrailingZeros returns the amount as the AEAT huella spec reads
// numeric fields: "one or two decimal positions, trailing zeros having no
// relevance" (especificaciones huella v0.1.2, page 6). E.g. 21.40 -> 21.4,
// 21.00 -> 21.
func (i Importe) WithoutTrailingZeros() string {
	intPart, decPart, _ := strings.Cut(i.value, importeDot)
	decPart = strings.TrimRight(decPart, "0")
	if decPart == "" {
		return intPart
	}
	return intPart + importeDot + decPart
}

// IsZero reports whether the amount was never built.
func (i Importe) IsZero() bool {
	return i.value == ""
}

func (i Importe) String() string {
	return i.value
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(importeDigits, r) {
			return false
		}
	}
	return true
}

func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}
