package xmlalta

import (
	"errors"
	"fmt"
)

// CancelledInvoiceID identifies the invoice being cancelled (IDFactura of an
// anulacion; every field names the *cancelled* invoice).
type CancelledInvoiceID struct {
	IssuerID       string `xml:"sum1:IDEmisorFacturaAnulada"`
	Number         string `xml:"sum1:NumSerieFacturaAnulada"`
	ExpeditionDate string `xml:"sum1:FechaExpedicionFacturaAnulada"` // DD-MM-AAAA
}

// CancellationRecord is one billing record of type anulacion
// (RegistroAnulacion): it cancels a previously issued invoice and is chained
// like any other record. Records are immutable once remitted.
type CancellationRecord struct {
	Version           string             `xml:"sum1:IDVersion"`            // fixed "1.0"
	InvoiceID         CancelledInvoiceID `xml:"sum1:IDFactura"`
	Subsanacion       string             `xml:"sum1:Subsanacion,omitempty"`   // optional "S"
	PreviousRejection string             `xml:"sum1:RechazoPrevio,omitempty"` // optional "S"
	Chaining          *Chaining          `xml:"sum1:Encadenamiento,omitempty"`
	System            SoftwareSystem     `xml:"sum1:SistemaInformatico"`
	GeneratedAt       string             `xml:"sum1:FechaHoraHusoGenRegistro"` // ISO 8601 with offset
	FingerprintType   string             `xml:"sum1:TipoHuella"`               // fixed "01"
	Fingerprint       string             `xml:"sum1:Huella"`                   // SHA-256, uppercase hex, 64 chars
}

func (CancellationRecord) recordKind() {}

// NewCancellationRecord returns an empty anulacion with the fixed schema
// fields (IDVersion "1.0", TipoHuella "01") already set.
func NewCancellationRecord() CancellationRecord {
	return CancellationRecord{Version: IDVersion, FingerprintType: FingerprintTypeSHA256}
}

// Validate applies the anulacion business rules and returns every violation
// joined in a single Spanish error (nil when valid).
func (r CancellationRecord) Validate() error {
	var errs []error
	errs = append(errs, validateVersion(r.Version)...)
	if r.InvoiceID.IssuerID == "" {
		errs = append(errs, errors.New("el identificador del emisor de la factura anulada (IDEmisorFacturaAnulada) es obligatorio"))
	}
	if r.InvoiceID.Number == "" {
		errs = append(errs, errors.New("el número de serie de la factura anulada (NumSerieFacturaAnulada) es obligatorio"))
	}
	if !validDate(r.InvoiceID.ExpeditionDate) {
		errs = append(errs, fmt.Errorf("la fecha de expedición de la factura anulada debe tener formato DD-MM-AAAA: %q", r.InvoiceID.ExpeditionDate))
	}
	errs = append(errs, validateRecordFooter(r.Chaining, r.System, r.GeneratedAt, r.FingerprintType, r.Fingerprint)...)
	return errors.Join(errs...)
}
