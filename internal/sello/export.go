package sello

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"time"

	"facturable/internal/sello/persistence"
)

// ExportLedger produces the period CSV for the tax advisor (issued-invoice
// book with its AEAT state). The two trailing columns carry the client NIF
// and name — model 303 needs the recipient identity, not only the emisor
// totals. Available ALWAYS, with or without a valid license: the user is
// never held hostage by their own data.
func ExportLedger(filas []persistence.LedgerRow) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{"Serie", "Date", "Tipo", "Base+Cuota", "CuotaTotal",
		"ImporteTotal", "EstadoAEAT", "CSV", "Huella", "Remitido",
		"NIFCliente", "NombreCliente"}
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, f := range filas {
		if err := w.Write([]string{
			sanitizeCSVField(f.Serie), sanitizeCSVField(f.Date), sanitizeCSVField(f.Tipo),
			sanitizeCSVField(fmt.Sprintf("%s|%s", f.Cuota, f.Importe)),
			sanitizeCSVField(f.Cuota), sanitizeCSVField(f.Importe),
			sanitizeCSVField(f.Estado), sanitizeCSVField(f.CSV),
			sanitizeCSVField(f.Huella), sanitizeCSVField(f.Remitido),
			sanitizeCSVField(f.ClientNIF), sanitizeCSVField(f.ClientName),
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// sanitizeCSVField neutralizes CSV formula injection (audit network/input
// #3): a field beginning with =, +, - or @ leaves the agent prefixed with a
// single quote, so Excel/LibreOffice render it as text instead of executing
// it when the tax advisor opens the exported book. Ledger data partly comes
// from customer CSVs, so any field is potentially hostile.
func sanitizeCSVField(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@':
		return "'" + v
	default:
		return v
	}
}

// ExportPeriod vuelca el libro de un rango de fechas (DD-MM-AAAA).
func (s *Service) ExportPeriod(desde, hasta string) ([]byte, int, error) {
	d, err := time.Parse("02-01-2006", desde)
	if err != nil {
		return nil, 0, fmt.Errorf("fecha inicial %q no válida (usa DD-MM-AAAA): %v", desde, err)
	}
	h, err := time.Parse("02-01-2006", hasta)
	if err != nil {
		return nil, 0, fmt.Errorf("fecha final %q no válida (usa DD-MM-AAAA): %v", hasta, err)
	}
	if h.Before(d) {
		return nil, 0, fmt.Errorf("la fecha final (%s) es anterior a la inicial (%s)", hasta, desde)
	}
	filas, err := s.Store.Ledger(s.EmisorNIF, 0, 1_000_000_000, 1_000_000)
	if err != nil {
		// persistence detail stays server-side (audit #8)
		return nil, 0, fmt.Errorf("%w: %v", persistence.ErrInternal, err)
	}
	seleccion := filtrarPeriodo(filas, d, h)
	b, err := ExportLedger(seleccion)
	return b, len(seleccion), err
}

func filtrarPeriodo(filas []persistence.LedgerRow, desde, hasta time.Time) []persistence.LedgerRow {
	var out []persistence.LedgerRow
	for _, f := range filas {
		fecha, err := time.Parse("02-01-2006", f.Date)
		if err != nil {
			continue
		}
		if !fecha.Before(desde) && !fecha.After(hasta) {
			out = append(out, f)
		}
	}
	return out
}
