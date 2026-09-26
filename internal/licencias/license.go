// Package licencias holds the pure licensing domain: Ed25519-signed license
// tokens and the "never hostage" gating rule. No IO, no clock, no network:
// the current time is always injected by the caller.
package licencias

import (
	"fmt"
	"strings"
	"time"
)

// DefaultGraceDays is the offline grace period granted after license expiry:
// invoicing cannot be held hostage to a renewal (INFORME-NEGOCIO §1.4).
const DefaultGraceDays = 7

// Plan is a commercial plan of a license.
type Plan string

const (
	PlanBasico   Plan = "basico"
	PlanPro      Plan = "pro"
	PlanGestoria Plan = "gestoria"
)

// Valid reports whether p is one of the known commercial plans.
func (p Plan) Valid() bool {
	switch p {
	case PlanBasico, PlanPro, PlanGestoria:
		return true
	default:
		return false
	}
}

// License is a signed entitlement to use the agent for a set of taxpayers
// (NIFs) until ExpiresAt, plus an offline grace period of GraceDays.
type License struct {
	LicenseID string
	Plan      Plan
	NIFs      []string
	ExpiresAt time.Time
	GraceDays int
	IssuedAt  time.Time
}

// NewLicense builds a License; a zero GraceDays means "use the default".
func NewLicense(id string, plan Plan, nifs []string, issuedAt, expiresAt time.Time, graceDays int) License {
	if graceDays == 0 {
		graceDays = DefaultGraceDays
	}
	if nifs == nil {
		nifs = []string{}
	}
	return License{
		LicenseID: id,
		Plan:      plan,
		NIFs:      nifs,
		ExpiresAt: expiresAt,
		GraceDays: graceDays,
		IssuedAt:  issuedAt,
	}
}

// Validate checks the license is complete and coherent.
func (l License) Validate() error {
	if l.LicenseID == "" {
		return fmt.Errorf("licencia no válida: falta el identificador de licencia")
	}
	if !l.Plan.Valid() {
		return fmt.Errorf("licencia no válida: plan desconocido %q", string(l.Plan))
	}
	if l.ExpiresAt.IsZero() {
		return fmt.Errorf("licencia no válida: falta la fecha de caducidad")
	}
	if l.IssuedAt.IsZero() {
		return fmt.Errorf("licencia no válida: falta la fecha de emisión")
	}
	if !l.ExpiresAt.After(l.IssuedAt) {
		return fmt.Errorf("licencia no válida: la caducidad (%s) debe ser posterior a la emisión (%s)",
			l.ExpiresAt.Format(dateLayout), l.IssuedAt.Format(dateLayout))
	}
	if l.GraceDays < 0 {
		return fmt.Errorf("licencia no válida: los días de gracia no pueden ser negativos")
	}
	return nil
}

// AuthorizesNIF reports whether the license covers a taxpayer NIF.
// An empty NIF list means unrestricted; otherwise the NIF must be listed
// (comparison ignores case and surrounding spaces).
func (l License) AuthorizesNIF(nif string) bool {
	if len(l.NIFs) == 0 {
		return true
	}
	wanted := normalizeNIF(nif)
	if wanted == "" {
		return false
	}
	for _, allowed := range l.NIFs {
		if normalizeNIF(allowed) == wanted {
			return true
		}
	}
	return false
}

func normalizeNIF(nif string) string {
	return strings.ToUpper(strings.TrimSpace(nif))
}

// dateLayout is the human-facing date format used in error messages (DD-MM-AAAA).
const dateLayout = "02-01-2006"
