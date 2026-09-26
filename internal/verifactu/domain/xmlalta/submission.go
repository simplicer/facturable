// Package xmlalta serializes Veri*Factu billing-record submissions
// (RegFactuSistemaFacturacion) to the exact SOAP XML contract of the AEAT
// "Sistemas Informáticos de Facturación" web services specification v1.0.3.
//
// The package is pure domain code: no I/O, no clock, no environment access.
// Serialization (XML) and business validation (Validate) are separate
// concerns; the application layer composes both before remitting a batch.
package xmlalta

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// Namespace URIs copied from the official message examples (spec v1.0.3,
// section 9.1.1). The PDF text wraps the URLs across lines; here they are
// joined as the schema names them.
const (
	NamespaceSOAPEnvelope = "http://schemas.xmlsoap.org/soap/envelope/"
	NamespaceSuministroLR = "https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroLR.xsd"
	NamespaceSuministroInfo = "https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroInformacion.xsd"
	NamespaceXMLDSig      = "http://www.w3.org/2000/09/xmldsig#"
)

// Fixed values of the 1.0 schema.
const (
	// IDVersion is the fixed schema version of every record ("1.0").
	IDVersion = "1.0"
	// FingerprintTypeSHA256 is the only fingerprint type of the 1.0 schema ("01").
	FingerprintTypeSHA256 = "01"
	// MaxBreakdownLines is the maximum number of Desglose lines per record.
	MaxBreakdownLines = 12
)

// Issuer is the issuing party of the submission (ObligadoEmision).
type Issuer struct {
	Name  string // sum1:NombreRazon
	TaxID string // sum1:NIF
}

// Record is one billing record inside a submission: either a registration
// (RegistroAlta) or a cancellation (RegistroAnulacion).
type Record interface {
	recordKind()
}

// Submission is a remittance message: the issuing party plus the ordered list
// of billing records being submitted in the SOAP body.
type Submission struct {
	Issuer  Issuer
	Records []Record
}

// NewSubmission starts a remittance for the issuer with the given records.
func NewSubmission(issuer Issuer, records ...Record) Submission {
	return Submission{Issuer: issuer, Records: records}
}

// Add appends a record to the submission.
func (s *Submission) Add(r Record) {
	s.Records = append(s.Records, r)
}

// Validate applies the business rules of the message and returns every
// violation joined in a single Spanish error (nil when valid).
func (s Submission) Validate() error {
	var errs []error
	if s.Issuer.Name == "" {
		errs = append(errs, errors.New("el nombre o razón social del obligado de emisión es obligatorio"))
	}
	if s.Issuer.TaxID == "" {
		errs = append(errs, errors.New("el NIF del obligado de emisión es obligatorio"))
	}
	if len(s.Records) == 0 {
		errs = append(errs, errors.New("la remisión debe incluir al menos un registro de facturación"))
	}
	for i, record := range s.Records {
		switch r := record.(type) {
		case RegistrationRecord:
			errs = append(errs, withRecord(r.Validate(), i)...)
		case CancellationRecord:
			errs = append(errs, withRecord(r.Validate(), i)...)
		}
	}
	return errors.Join(errs...)
}

// withRecord prefixes the record position (1-based) to each violation.
func withRecord(err error, position int) []error {
	if err == nil {
		return nil
	}
	prefix := fmt.Sprintf("registro %d: ", position+1)
	lines := strings.Split(err.Error(), "\n")
	out := make([]error, 0, len(lines))
	for _, line := range lines {
		out = append(out, errors.New(prefix+line))
	}
	return out
}

// XML serializes the submission to the SOAP envelope of the remittance
// message (RegFactuSistemaFacturacion). It reproduces the official message
// example element by element; call Validate separately for business rules.
func (s Submission) XML() (string, error) {
	records := make([]registroFactura, 0, len(s.Records))
	for _, record := range s.Records {
		switch r := record.(type) {
		case RegistrationRecord:
			records = append(records, registroFactura{Alta: &r})
		case CancellationRecord:
			records = append(records, registroFactura{Anulacion: &r})
		default:
			return "", fmt.Errorf("tipo de registro de facturación no soportado: %T", record)
		}
	}

	envelope := soapEnvelope{
		XmlnsSoapenv: NamespaceSOAPEnvelope,
		XmlnsSum:     NamespaceSuministroLR,
		XmlnsSum1:    NamespaceSuministroInfo,
		XmlnsXd:      NamespaceXMLDSig,
		Body: soapBody{
			Message: soapMessage{
				Header:  soapCabecera{Issuer: obligadoEmision(s.Issuer)},
				Records: records,
			},
		},
	}
	out, err := xml.MarshalIndent(envelope, "", "   ")
	if err != nil {
		return "", fmt.Errorf("no se pudo serializar la remisión de registros de facturación: %w", err)
	}
	return xml.Header + string(out), nil
}

// soapEnvelope is the wire shape of the remittance. Prefix tags are declared
// as xmlns attributes exactly like the official example.
type soapEnvelope struct {
	XMLName      xml.Name   `xml:"soapenv:Envelope"`
	XmlnsSoapenv string     `xml:"xmlns:soapenv,attr"`
	XmlnsSum     string     `xml:"xmlns:sum,attr"`
	XmlnsSum1    string     `xml:"xmlns:sum1,attr"`
	XmlnsXd      string     `xml:"xmlns:xd,attr"`
	Header       soapHeader `xml:"soapenv:Header"`
	Body         soapBody   `xml:"soapenv:Body"`
}

type soapHeader struct{}

type soapBody struct {
	Message soapMessage `xml:"sum:RegFactuSistemaFacturacion"`
}

type soapMessage struct {
	Header  soapCabecera      `xml:"sum:Cabecera"`
	Records []registroFactura `xml:"sum:RegistroFactura"`
}

// soapCabecera wraps the issuing party with the ObligadoEmision element,
// exactly as the official example nests it inside Cabecera.
type soapCabecera struct {
	Issuer obligadoEmision `xml:"sum1:ObligadoEmision"`
}

type obligadoEmision struct {
	Name  string `xml:"sum1:NombreRazon"`
	TaxID string `xml:"sum1:NIF"`
}

// registroFactura wraps one record with its RegistroFactura container so a
// submission can carry registrations and cancellations in one batch.
type registroFactura struct {
	Alta      *RegistrationRecord `xml:"sum1:RegistroAlta,omitempty"`
	Anulacion *CancellationRecord `xml:"sum1:RegistroAnulacion,omitempty"`
}
