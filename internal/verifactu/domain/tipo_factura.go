package domain

import "fmt"

// TipoFactura classifies the billing record (L10 "TipoFactura" of the AEAT
// register design: F1-F4 invoices, R1-R5 rectifications).
type TipoFactura string

const (
	// FacturaCompleta is a full invoice.
	FacturaCompleta TipoFactura = "F1"
	// FacturaSimplificada is a simplified invoice/ticket.
	FacturaSimplificada TipoFactura = "F2"
	// FacturaSustitutiva replaces previously issued simplified invoices.
	FacturaSustitutiva TipoFactura = "F3"
	// FacturaPorErrorFundado is an invoice issued due to a justified error
	// on behalf of the recipient (factura indicativa, error fundado).
	FacturaPorErrorFundado TipoFactura = "F4"
	// Rectificativa1..5 are the correction classes R1-R5 of the register design.
	Rectificativa1 TipoFactura = "R1"
	Rectificativa2 TipoFactura = "R2"
	Rectificativa3 TipoFactura = "R3"
	Rectificativa4 TipoFactura = "R4"
	Rectificativa5 TipoFactura = "R5"
)

// NewTipoFactura validates an AEAT invoice type code.
func NewTipoFactura(raw string) (TipoFactura, error) {
	value := TipoFactura(trimSpaces(raw))
	switch value {
	case FacturaCompleta, FacturaSimplificada, FacturaSustitutiva, FacturaPorErrorFundado,
		Rectificativa1, Rectificativa2, Rectificativa3, Rectificativa4, Rectificativa5:
		return value, nil
	}
	return "", fmt.Errorf("el tipo de factura %q no es válido (códigos válidos: F1, F2, F3, F4, R1, R2, R3, R4, R5)", raw)
}

// String returns the AEAT code.
func (t TipoFactura) String() string {
	return string(t)
}

// IsRectificativa reports whether the type opens a correction line (R1-R5).
func (t TipoFactura) IsRectificativa() bool {
	switch t {
	case Rectificativa1, Rectificativa2, Rectificativa3, Rectificativa4, Rectificativa5:
		return true
	}
	return false
}
