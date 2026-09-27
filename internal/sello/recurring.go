package sello

import (
	"errors"

	"facturable/internal/sello/ingest"
)

// RecurringClient is one frequent client billed a fixed monthly amount:
// the payload of POST /v1/recurring (same five fields as the quick form).
type RecurringClient struct {
	ClientName string `json:"cliente"`
	ClientNIF  string `json:"nif"`
	Concept    string `json:"concepto"`
	Base       string `json:"base"`
	VATRate    string `json:"iva,omitempty"` // default "21"
}

// RecurringStamped is the per-invoice outcome of a recurring batch: the
// fresh series on success, the user-facing reason on failure.
type RecurringStamped struct {
	Cliente string `json:"cliente"`
	Serie   string `json:"serie,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RecurringSummary resumes a recurring batch: how many stamped, how many
// failed and the detail per client.
type RecurringSummary struct {
	Selladas int                `json:"selladas"`
	Fallidas int                `json:"fallidas"`
	Facturas []RecurringStamped `json:"facturas"`
}

// RecurringBatch is the list of frequent clients with a fixed monthly
// amount; StampAll generates + stamps the whole list in one sequence.
type RecurringBatch struct {
	srv     *Service
	Clients []RecurringClient
}

// NewRecurringBatch binds a frequent-client list to the stamping service.
func NewRecurringBatch(srv *Service, clients []RecurringClient) *RecurringBatch {
	return &RecurringBatch{srv: srv, Clients: clients}
}

// StampAll generates + stamps every recurring invoice in sequence, so each
// one chains on the previous. One failing client does not abort the batch:
// its reason lands in the summary and the rest go on.
func (b *RecurringBatch) StampAll() *RecurringSummary {
	sum := &RecurringSummary{Facturas: make([]RecurringStamped, 0, len(b.Clients))}
	for _, c := range b.Clients {
		line := RecurringStamped{Cliente: c.ClientName}
		serie, err := b.stampOne(c)
		if err == nil {
			line.Serie = serie
			sum.Selladas++
		} else {
			line.Error = err.Error()
			sum.Fallidas++
		}
		sum.Facturas = append(sum.Facturas, line)
	}
	return sum
}

// stampOne expands one client to a quick-form draft (series computed from
// the ledger as of now) and stamps it through the IngestDrafts pipeline.
func (b *RecurringBatch) stampOne(c RecurringClient) (string, error) {
	draft, err := (QuickInvoice{
		ClientName: c.ClientName, ClientNIF: c.ClientNIF,
		Concept: c.Concept, Base: c.Base, VATRate: c.VATRate,
	}).ToDraft(b.srv.lastSeries(), b.srv.clock())
	if err != nil {
		return "", err
	}
	res, err := b.srv.IngestDrafts([]ingest.Draft{draft})
	if err != nil {
		return "", err
	}
	if res.Sellados == 0 {
		return "", errors.New(res.Errores[0].Detalle)
	}
	return draft.Series, nil
}
