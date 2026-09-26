package domain

import (
	"fmt"
	"time"
)

// AEAT date field layout (FechaExpedicionFactura is DD-MM-AAAA).
const (
	aeatDateLayout = "02-01-2006"
	isoDateLayout  = "2006-01-02"
)

// FechaExpedicion is the date an invoice was issued, exchanged with the AEAT
// as DD-MM-AAAA. It carries no time and no wall clock: it is a pure date.
type FechaExpedicion struct {
	value time.Time
}

// NewFechaExpedicion parses a date in DD-MM-AAAA (AEAT) or AAAA-MM-DD (ISO).
func NewFechaExpedicion(raw string) (FechaExpedicion, error) {
	trimmed := trimSpaces(raw)
	if trimmed == "" {
		return FechaExpedicion{}, fmt.Errorf("la fecha de expedición no puede estar vacía")
	}
	for _, layout := range []string{aeatDateLayout, isoDateLayout} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return FechaExpedicion{value: parsed}, nil
		}
	}
	return FechaExpedicion{}, fmt.Errorf(
		"la fecha de expedición %q no es válida (formatos aceptados: DD-MM-AAAA o AAAA-MM-DD)", raw)
}

// NewFechaExpedicionFromTime takes the calendar day of an instant in its own
// timezone (the record timezone), never the wall clock.
func NewFechaExpedicionFromTime(stamp time.Time) FechaExpedicion {
	year, month, day := stamp.Date()
	return FechaExpedicion{value: time.Date(year, month, day, 0, 0, 0, 0, stamp.Location())}
}

// Fecha returns the date formatted as DD-MM-AAAA for AEAT fields.
func (f FechaExpedicion) Fecha() string {
	return f.value.Format(aeatDateLayout)
}

// IsZero reports whether the date was never built.
func (f FechaExpedicion) IsZero() bool {
	return f.value.IsZero()
}

func (f FechaExpedicion) String() string {
	return f.Fecha()
}
