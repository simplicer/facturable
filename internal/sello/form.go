package sello

import (
	"fmt"
	"strings"
	"time"

	"facturable/internal/sello/ingest"
	"facturable/internal/verifactu/application"
)

// QuickInvoice is the 5-field human form (bloqueante MVP): the agent fills
// every default so the user only answers WHO, WHAT and HOW MUCH.
type QuickInvoice struct {
	ClientName string `json:"cliente"`
	ClientNIF  string `json:"nif"`
	Concept    string `json:"concepto"`
	Base       string `json:"base"`
	VATRate    string `json:"iva,omitempty"` // default "21"
}

// ToDraft expands the quick form into a full ingest.Draft with smart
// defaults: series auto-numbered per taxpayer, date = today, tipo = F1,
// IVA S1 general regime, cuota computed from base×rate in exact cents.
func (q QuickInvoice) ToDraft(lastSeries string, now time.Time) (ingest.Draft, error) {
	if strings.TrimSpace(q.ClientName) == "" {
		return ingest.Draft{}, fmt.Errorf("falta el nombre del cliente")
	}
	if strings.TrimSpace(q.Concept) == "" {
		return ingest.Draft{}, fmt.Errorf("falta el concepto")
	}
	rate := q.VATRate
	if rate == "" {
		rate = "21"
	}
	base, err := application.ToCents(q.Base)
	if err != nil {
		return ingest.Draft{}, fmt.Errorf("base: %v", err)
	}
	// audit abuse #3: the quick form stamps ordinary F1 invoices only, so a
	// zero or negative base is refused up front — a negative base belongs to
	// an R1-R5 rectificativa, which this form never produces.
	if base <= 0 {
		return ingest.Draft{}, fmt.Errorf("la base imponible debe ser mayor que cero (una base negativa solo cabe en una rectificativa R1-R5)")
	}
	rateC, err := application.ToCents(rate)
	if err != nil || rateC < 0 || rateC > 3000 {
		return ingest.Draft{}, fmt.Errorf("tipo de IVA %q no válido", rate)
	}
	cuota := base * rateC / 10000
	return ingest.Draft{
		Series:      nextSeries(lastSeries, now),
		Date:        now.Format("02-01-2006"),
		Tipo:        "F1",
		Description: q.Concept,
		ClientNIF:   strings.TrimSpace(q.ClientNIF),
		ClientName:  strings.TrimSpace(q.ClientName),
		Breakdowns: []ingest.BreakdownDraft{{
			TaxRate: rate,
			Base:    application.FromCents(base),
			Fee:     application.FromCents(cuota),
		}},
	}, nil
}

// nextSeries generates the next sequential series number for the year:
// F-2027-0001, F-2027-0002, ... (or starts fresh if year changed).
func nextSeries(last string, now time.Time) string {
	prefix := fmt.Sprintf("F-%d-", now.Year())
	if strings.HasPrefix(last, prefix) {
		n := 0
		for _, c := range last[len(prefix):] {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			return fmt.Sprintf("%s%04d", prefix, n+1)
		}
	}
	return fmt.Sprintf("%s0001", prefix)
}
