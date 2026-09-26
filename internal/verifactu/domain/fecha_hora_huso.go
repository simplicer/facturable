package domain

import (
	"fmt"
	"time"
)

// FechaHoraHusoGenRegistro is ISO-8601 with a mandatory UTC offset,
// as required for FechaHoraHusoGenRegistro (e.g. 2024-11-04T12:36:39+01:00).
const fechaHoraHusoLayout = time.RFC3339

// FechaHoraHuso is the timezone-aware instant a record was generated inside
// the SIF. It never reads the wall clock: application code injects the
// instant; the domain only validates, formats and hashes it.
//
// The exact string is preserved because the AEAT recomputes the huella from
// the literal XML value: rendering "+00:00" as "Z" (Go's RFC3339 default)
// would silently change the hash input.
type FechaHoraHuso struct {
	stamp time.Time // parsed instant, kept for ordering/arithmetic needs
	raw   string    // validated literal representation
}

// NewFechaHoraHuso parses an ISO-8601 instant with explicit offset and
// preserves its literal form.
func NewFechaHoraHuso(raw string) (FechaHoraHuso, error) {
	trimmed := trimSpaces(raw)
	if trimmed == "" {
		return FechaHoraHuso{}, fmt.Errorf("la fecha y hora de generación del chainRow no puede estar vacía")
	}
	parsed, err := time.Parse(fechaHoraHusoLayout, trimmed)
	if err != nil {
		return FechaHoraHuso{}, fmt.Errorf(
			"la fecha y hora de generación %q no es válida (se requiere ISO-8601 con huso horario, p. ej. 2024-11-04T12:36:39+01:00)", raw)
	}
	return FechaHoraHuso{stamp: parsed, raw: trimmed}, nil
}

// NewFechaHoraHusoFromTime wraps an injected instant (from the application
// clock port), rendering it with its offset.
// NewFechaHoraHusoFromTime renders the instant with a numeric offset
// (+hh:mm). A UTC instant (which formats as 'Z') is converted to
// Europe/Madrid so the wire value always carries a numeric offset, as the
// AEAT schema requires (audit finding: containers run in UTC).
func NewFechaHoraHusoFromTime(stamp time.Time) FechaHoraHuso {
	if stamp.Location() == time.UTC {
		madrid, err := time.LoadLocation("Europe/Madrid")
		if err == nil {
			stamp = stamp.In(madrid)
		}
	}
	return FechaHoraHuso{stamp: stamp, raw: stamp.Format(fechaHoraHusoLayout)}
}

// ISO8601 renders the instant exactly as it goes into
// FechaHoraHusoGenRegistro and into the huella input.
func (f FechaHoraHuso) ISO8601() string {
	return f.raw
}

// IsZero reports whether the instant was never set.
func (f FechaHoraHuso) IsZero() bool {
	return f.raw == ""
}

func (f FechaHoraHuso) String() string {
	return f.raw
}
