package sello

import (
	"errors"
	"fmt"
	"strings"

	"facturable/internal/sello/ingest"
	"facturable/internal/verifactu/application"
)

// ErrRecordNotFound marks a record UUID absent from the ledger.
var ErrRecordNotFound = errors.New("no existe el registro solicitado")

// DuplicateResult resumes a duplicate stamping: the fresh copy and which
// original it came from.
type DuplicateResult struct {
	Sellada  bool   `json:"sellada"`
	Serie    string `json:"serie"`
	Fecha    string `json:"fecha"`
	Original string `json:"original"`
	Cuota    string `json:"cuota"`
	Total    string `json:"total"` // base + cuota
}

// DuplicateInvoice rebuilds the quick form of the record identified by
// recordUUID (the caller sends the client's last invoice) from its stored
// submission XML and stamps a copy dated today with a fresh series, through
// the same IngestDrafts pipeline as every other stamping path.
func (s *Service) DuplicateInvoice(recordUUID string) (*DuplicateResult, error) {
	if recordUUID == "" {
		return nil, ErrRecordNotFound
	}
	raw, ok, err := s.Store.XMLByUUID(recordUUID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrRecordNotFound
	}
	draft, err := quickInvoiceFromXML(raw).ToDraft(s.lastSeries(), s.clock())
	if err != nil {
		return nil, err
	}
	res, err := s.IngestDrafts([]ingest.Draft{draft})
	if err != nil {
		return nil, err
	}
	if res.Sellados == 0 {
		return nil, fmt.Errorf("no se pudo duplicar: %s", res.Errores[0].Detalle)
	}
	base, _ := application.ToCents(draft.Breakdowns[0].Base)
	cuota, _ := application.ToCents(draft.Breakdowns[0].Fee)
	return &DuplicateResult{
		Sellada: true, Serie: draft.Series, Fecha: draft.Date, Original: recordUUID,
		Cuota: draft.Breakdowns[0].Fee, Total: application.FromCents(base + cuota),
	}, nil
}

// quickInvoiceFromXML rebuilds the 5-field quick form from a stored
// submission XML. Quick-form invoices carry exactly one VAT breakdown;
// multi-breakdown records duplicate their first one.
func quickInvoiceFromXML(raw string) QuickInvoice {
	client := xmlSegment(raw, "sum1:IDDestinatario")
	breakdown := xmlSegment(raw, "sum1:DetalleDesglose")
	return QuickInvoice{
		ClientName: strings.TrimSpace(xmlSegment(client, "sum1:NombreRazon")),
		ClientNIF:  strings.TrimSpace(xmlSegment(client, "sum1:NIF")),
		Concept:    strings.TrimSpace(xmlSegment(raw, "sum1:DescripcionOperacion")),
		Base:       strings.TrimSpace(xmlSegment(breakdown, "sum1:BaseImponibleOimporteNoSujeto")),
		VATRate:    strings.TrimSpace(xmlSegment(breakdown, "sum1:TipoImpositivo")),
	}
}

// xmlSegment returns the inner text of the first <tag>…</tag> block (plain
// extraction, the same trick the simulated submitter uses: the envelope
// carries fixed namespace prefixes).
func xmlSegment(s, tag string) string {
	start := "<" + tag + ">"
	end := "</" + tag + ">"
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	from := s[i+len(start):]
	j := strings.Index(from, end)
	if j < 0 {
		return ""
	}
	return from[:j]
}
