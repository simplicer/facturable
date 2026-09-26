package licencias

import (
	"errors"
	"fmt"
	"time"
)

// Action is a protected capability the license gates.
type Action string

const (
	// ActionStampNew is stamping (sealing) a NEW invoice into the ledger.
	// This is the only action an expired license blocks.
	ActionStampNew Action = "stamp_new"
	// ActionOpenData is opening and reading the user's own recorded data.
	ActionOpenData Action = "open_data"
	// ActionExport is exporting the user's own ledger (CSV/ZIP).
	ActionExport Action = "export"
)

var (
	// ErrLicenseExpired means the license and its grace period are over.
	ErrLicenseExpired = errors.New("la licencia ha caducado")
	// ErrUnknownAction means the requested capability is not recognized.
	ErrUnknownAction = errors.New("acción no reconocida")
)

// GraceEnd returns the last instant the license may still stamp new invoices:
// ExpiresAt plus GraceDays of offline grace.
func (l License) GraceEnd() time.Time {
	return l.ExpiresAt.Add(time.Duration(l.GraceDays) * 24 * time.Hour)
}

// Gate enforces the "never hostage" business rule (INFORME-NEGOCIO §1.4):
// an expired license only blocks stamping NEW invoices, and only after the
// offline grace period; opening the user's data and exporting it are ALWAYS
// allowed. The current time is injected, so the check needs no clock and no
// network.
func Gate(l License, now time.Time, action Action) error {
	switch action {
	case ActionOpenData, ActionExport:
		return nil
	case ActionStampNew:
		if now.After(l.GraceEnd()) {
			return fmt.Errorf("%w: el periodo de gracia terminó el %s; no se pueden sellar facturas nuevas, pero abrir y exportar sus datos siguen disponibles",
				ErrLicenseExpired, l.GraceEnd().Format(dateLayout))
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, string(action))
	}
}
