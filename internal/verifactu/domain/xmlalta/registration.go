package xmlalta

import (
	"errors"
	"fmt"
)

// InvoiceID identifies the invoice being registered (IDFactura of an alta).
type InvoiceID struct {
	IssuerID       string `xml:"sum1:IDEmisorFactura"`        // NIF or foreign ID
	Number         string `xml:"sum1:NumSerieFactura"`        // ≤ 60 chars
	ExpeditionDate string `xml:"sum1:FechaExpedicionFactura"` // DD-MM-AAAA
}

// RectifiedID identifies a rectified invoice inside R1-R5.
type RectifiedID struct {
	Series string `xml:"sum1:NumSerieFacturaRectificada"`
	Date   string `xml:"sum1:FechaExpedicionFacturaRectificada"` // DD-MM-AAAA
}

// Recipient is one invoice recipient (IDDestinatario).
type Recipient struct {
	Name  string `xml:"sum1:NombreRazon"`
	TaxID string `xml:"sum1:NIF"`
}

// BreakdownLine is one tax line of the invoice breakdown (DetalleDesglose).
type BreakdownLine struct {
	Tax                    string `xml:"sum1:Impuesto,omitempty"`            // optional, e.g. "01" IVA
	RegimeKey              string `xml:"sum1:ClaveRegimen"`                  // e.g. "01"
	OperationQualification string `xml:"sum1:CalificacionOperacion"`         // e.g. "S1"
	TaxRate                string `xml:"sum1:TipoImpositivo,omitempty"`      // optional decimal
	TaxBase                string `xml:"sum1:BaseImponibleOimporteNoSujeto"` // decimal
	TaxFee                 string `xml:"sum1:CuotaRepercutida"`              // decimal
}

// Chaining links the record to the billing chain (Encadenamiento): either it
// is the first record of the chain or it points at the previous record.
type Chaining struct {
	FirstRecord    string          `xml:"sum1:PrimerRegistro,omitempty"` // "S" when first
	PreviousRecord *PreviousRecord `xml:"sum1:RegistroAnterior,omitempty"`
}

// PreviousRecord is the chained predecessor of a record.
type PreviousRecord struct {
	IssuerID       string `xml:"sum1:IDEmisorFactura"`
	Number         string `xml:"sum1:NumSerieFactura"`
	ExpeditionDate string `xml:"sum1:FechaExpedicionFactura"` // DD-MM-AAAA
	Fingerprint    string `xml:"sum1:Huella"`                 // SHA-256, uppercase hex, 64 chars
}

// SoftwareSystem describes the producing billing system (SistemaInformatico).
type SoftwareSystem struct {
	Name                string `xml:"sum1:NombreRazon"` // producer
	TaxID               string `xml:"sum1:NIF"`         // producer
	SystemName          string `xml:"sum1:NombreSistemaInformatico"`
	SystemID            string `xml:"sum1:IdSistemaInformatico"` // exactly 2 characters
	Version             string `xml:"sum1:Version"`
	InstallationNumber  string `xml:"sum1:NumeroInstalacion"`
	VerifactuOnlyUse    string `xml:"sum1:TipoUsoPosibleSoloVerifactu"` // S/N
	MultiTaxpayerUse    string `xml:"sum1:TipoUsoPosibleMultiOT"`       // S/N
	MultipleOTIndicator string `xml:"sum1:IndicadorMultiplesOT"`        // S/N
}

// RegistrationRecord is one billing record of type alta (RegistroAlta).
// Records are immutable once remitted; corrections are new records.
type RegistrationRecord struct {
	Version              string          `xml:"sum1:IDVersion"` // fixed "1.0"
	InvoiceID            InvoiceID       `xml:"sum1:IDFactura"`
	IssuerName           string          `xml:"sum1:NombreRazonEmisor"`
	Subsanacion          string          `xml:"sum1:Subsanacion,omitempty"`                                    // optional "S"
	PreviousRejection    string          `xml:"sum1:RechazoPrevio,omitempty"`                                  // optional "S"/"X"
	InvoiceType          string          `xml:"sum1:TipoFactura"`                                              // F1..R5
	RectifiedType        string          `xml:"sum1:TipoRectificativa,omitempty"`                              // S/I (R1-R5)
	RectifiedInvoices    *[]RectifiedID  `xml:"sum1:FacturasRectificadas>sum1:IDFacturaRectificada,omitempty"` // pointer: nil omits the parent (encoding/xml ignores omitempty in chains)
	OperationDescription string          `xml:"sum1:DescripcionOperacion"`
	Recipients           []Recipient     `xml:"sum1:Destinatarios>sum1:IDDestinatario,omitempty"`
	Breakdown            []BreakdownLine `xml:"sum1:Desglose>sum1:DetalleDesglose,omitempty"` // 1..MaxBreakdownLines
	TotalTax             string          `xml:"sum1:CuotaTotal"`                              // decimal
	TotalAmount          string          `xml:"sum1:ImporteTotal"`                            // decimal
	SimplifiedInvoice    string          `xml:"sum1:FacturaSimplificada,omitempty"`           // optional "S"
	Chaining             *Chaining       `xml:"sum1:Encadenamiento,omitempty"`
	System               SoftwareSystem  `xml:"sum1:SistemaInformatico"`
	GeneratedAt          string          `xml:"sum1:FechaHoraHusoGenRegistro"` // ISO 8601 with offset
	FingerprintType      string          `xml:"sum1:TipoHuella"`               // fixed "01"
	Fingerprint          string          `xml:"sum1:Huella"`                   // SHA-256, uppercase hex, 64 chars
}

func (RegistrationRecord) recordKind() {}

// NewRegistrationRecord returns an empty alta with the fixed schema fields
// (IDVersion "1.0", TipoHuella "01") already set.
func NewRegistrationRecord() RegistrationRecord {
	return RegistrationRecord{Version: IDVersion, FingerprintType: FingerprintTypeSHA256}
}

// Validate applies the alta business rules and returns every violation joined
// in a single Spanish error (nil when valid).
func (r RegistrationRecord) Validate() error {
	errs := validateVersion(r.Version)
	errs = append(errs, validateInvoiceID(r.InvoiceID)...)
	if r.IssuerName == "" {
		errs = append(errs, errors.New("el nombre o razón social del emisor (NombreRazonEmisor) es obligatorio"))
	}
	if r.InvoiceType == "" {
		errs = append(errs, errors.New("el tipo de factura (TipoFactura) es obligatorio"))
	}
	if r.OperationDescription == "" {
		errs = append(errs, errors.New("la descripción de la operación (DescripcionOperacion) es obligatoria"))
	}
	errs = append(errs, validateBreakdown(r.Breakdown)...)
	errs = append(errs, validateAmountField(r.TotalTax, "la cuota total (CuotaTotal)")...)
	errs = append(errs, validateAmountField(r.TotalAmount, "el importe total (ImporteTotal)")...)
	errs = append(errs, validateRecordFooter(r.Chaining, r.System, r.GeneratedAt, r.FingerprintType, r.Fingerprint)...)
	return errors.Join(errs...)
}

func validateInvoiceID(id InvoiceID) []error {
	var errs []error
	if id.IssuerID == "" {
		errs = append(errs, errors.New("el identificador del emisor de la factura (IDEmisorFactura) es obligatorio"))
	}
	if id.Number == "" {
		errs = append(errs, errors.New("el número de series de la factura (NumSerieFactura) es obligatorio"))
	}
	if !validDate(id.ExpeditionDate) {
		errs = append(errs, fmt.Errorf("la fecha de expedición de la factura debe tener formato DD-MM-AAAA: %q", id.ExpeditionDate))
	}
	return errs
}

func validateBreakdown(lines []BreakdownLine) []error {
	var errs []error
	switch {
	case len(lines) == 0:
		errs = append(errs, errors.New("el desglose (Desglose) debe incluir al menos un detalle"))
	case len(lines) > MaxBreakdownLines:
		errs = append(errs, fmt.Errorf("el desglose (Desglose) no puede contener más de %d detalles: tiene %d", MaxBreakdownLines, len(lines)))
	}
	for i, line := range lines {
		position := i + 1
		if line.RegimeKey == "" {
			errs = append(errs, fmt.Errorf("el detalle %d del desglose debe indicar la clave del régimen (ClaveRegimen)", position))
		}
		if line.OperationQualification == "" {
			errs = append(errs, fmt.Errorf("el detalle %d del desglose debe indicar la calificación de la operación (CalificacionOperacion)", position))
		}
		if line.TaxRate != "" && !validAmount(line.TaxRate) {
			errs = append(errs, fmt.Errorf("el detalle %d del desglose tiene un tipo impositivo (TaxRate) no válido: %q", position, line.TaxRate))
		}
		errs = append(errs, validateAmountField(line.TaxBase, fmt.Sprintf("el detalle %d del desglose tiene una base imponible (BaseImponibleOimporteNoSujeto)", position))...)
		errs = append(errs, validateAmountField(line.TaxFee, fmt.Sprintf("el detalle %d del desglose tiene una cuota repercutida (CuotaRepercutida)", position))...)
	}
	return errs
}
