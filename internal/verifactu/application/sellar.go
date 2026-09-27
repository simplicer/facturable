package application

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"facturable/internal/verifactu/domain"
	"facturable/internal/verifactu/domain/xmlalta"
)

// SystemInfo identifies the SIF inside every chainRow (art. 15 OM: producer,
// 2-character IdSistemaInformatico, version and installation).
type SystemInfo struct {
	NombreProductor, NIFProductor string
	NombreSistema, IDSistema      string // IDSistema: exactamente 2 caracteres
	Version, NumeroInstalacion    string
}

// InvoiceDraft es la factura validada por la ingesta, lista para sellarse.
type InvoiceDraft struct {
	Series, Date, Tipo, Description string
	ClientNIF, ClientName           string
	Desgloses                       []BreakdownDraft
	// Rectificativa R1 (by difference): negative breakdowns/amounts and the
	// identified rectified invoices. Empty for ordinary F1.
	Rectificadas  []RectificadaRef
	Subsanacion   bool // re-stamp after an AEAT rejection (Subsanacion=S)
	RechazoPrevio bool
}

// RectificadaRef identifies an already-issued invoice being rectified.
type RectificadaRef struct{ Series, Date string }

// BreakdownDraft is one VAT line of the draft.
type BreakdownDraft struct {
	TaxRate, Base, Cuota string
}

// ChainState is the snapshot of the ledger the caller takes from the store:
//   - Last: identity (series/date/fingerprint) of the last chainRow, n-1.
//   - DeclaradoPorUltimo: the backlink n-1 embedded toward n-2 (nil when n-1
//     was the first record). It is what makes tampering detectable.
//   - Previous: the real identity of n-2 according to the store (nil when
//     absent).
//
// Last nil = empty chain.
type ChainState struct {
	Last               *ChainLink
	DeclaradoPorUltimo *ChainLink
	Previous           *ChainLink
}

// ChainLink identifica un chainRow: series, fecha y huella.
type ChainLink struct{ Serie, Date, Huella string }

// SealResult es todo lo que hay que persistir tras sellar.
type SealResult struct {
	XML string // complete SOAP envelope of the chainRow (single submission)

	NIF, Serie, Date, Tipo                 string
	Cuota, Importe, Generado, Huella       string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string // empty on the first record
	EsPrimero                              bool
}

// SealInvoice turns a draft into a chained billing chainRow with its
// submission XML. It verifies the n-1/n-2 chain jump before stamping
// (AEAT traceability FAQ) and computes totals by exact cent addition.
func SealInvoice(emisorNIF, emisorNombre string, b InvoiceDraft, sis SystemInfo, cadena ChainState, ahora time.Time) (*SealResult, error) {
	nif, err := domain.NewNIF(emisorNIF)
	if err != nil {
		return nil, fmt.Errorf("NIF del emisor: %v", err)
	}
	series, err := domain.NewSerieNumero(b.Series)
	if err != nil {
		return nil, fmt.Errorf("número de factura: %v", err)
	}
	fecha, err := domain.NewFechaExpedicion(b.Date)
	if err != nil {
		return nil, fmt.Errorf("fecha: %v", err)
	}
	tipo, err := domain.NewTipoFactura(b.Tipo)
	if err != nil {
		return nil, fmt.Errorf("tipo de factura: %v", err)
	}

	// Exact totals in cents (no floats).
	cuotaCent, baseCent := 0, 0
	lineas := make([]xmlalta.BreakdownLine, 0, len(b.Desgloses))
	for _, d := range b.Desgloses {
		base, err := toCents(d.Base)
		if err != nil {
			return nil, fmt.Errorf("base imponible de %s: %v", d.TaxRate, err)
		}
		cuota, err := toCents(d.Cuota)
		if err != nil {
			return nil, fmt.Errorf("cuota de %s: %v", d.TaxRate, err)
		}
		baseCent += base
		cuotaCent += cuota
		lineas = append(lineas, xmlalta.BreakdownLine{
			Tax:                    "01", // IVA
			RegimeKey:              "01", // general regime
			OperationQualification: "S1", // sujeta y no exenta
			TaxRate:                normalizeDecimal(d.TaxRate),
			TaxBase:                fromCents(base),
			TaxFee:                 fromCents(cuota),
		})
	}
	if len(lineas) == 0 {
		return nil, fmt.Errorf("la factura %s no tiene desgloses de IVA", b.Series)
	}
	if len(lineas) > 12 {
		return nil, fmt.Errorf("la factura %s supera los 12 desgloses admitidos por la AEAT", b.Series)
	}

	// Chain jump verification before computing the fingerprint.
	var previousHuella domain.Huella
	var encadenamiento xmlalta.Chaining
	switch {
	case cadena.Last == nil:
		encadenamiento = xmlalta.Chaining{FirstRecord: "S"}
	default:
		ult := toDomainLink(*cadena.Last)
		var declarado, ant *domain.Enlace
		if cadena.DeclaradoPorUltimo != nil {
			declarado = toDomainEnlace(*cadena.DeclaradoPorUltimo)
		}
		if cadena.Previous != nil {
			ant = toDomainEnlace(*cadena.Previous)
		}
		vigente := domain.LastChainRecord{Enlace: *ult, Referencia: declarado}
		if err := domain.VerificarSalto(vigente, ant); err != nil {
			return nil, fmt.Errorf("no se puede sellar: %v", err)
		}
		previousHuella = ult.Huella
		encadenamiento = xmlalta.Chaining{PreviousRecord: &xmlalta.PreviousRecord{
			IssuerID:       nif.Value(),
			Number:         ult.Serie.Value(),
			ExpeditionDate: ult.Date.Date(),
			Fingerprint:    ult.Huella.Value(),
		}}
	}

	generado := domain.NewFechaHoraHusoFromTime(ahora)
	huella, err := domain.HuellaRegistroAlta(domain.RegistroAltaHuella{
		Emisor: nif, Serie: series, Date: fecha, Tipo: tipo,
		Cuota:    mustTotal(fromCents(cuotaCent)),
		Importe:  mustTotal(fromCents(baseCent + cuotaCent)),
		Generado: generado, Previous: previousHuella,
	})
	if err != nil {
		return nil, err
	}

	reg := xmlalta.NewRegistrationRecord()
	reg.InvoiceID = xmlalta.InvoiceID{IssuerID: nif.Value(), Number: series.Value(), ExpeditionDate: fecha.Date()}
	reg.IssuerName = emisorNombre
	reg.InvoiceType = tipo.String()
	if len(b.Rectificadas) > 0 {
		if !tipo.IsRectificativa() {
			return nil, fmt.Errorf("la factura %s identifica rectificadas pero su tipo no es R1-R5", b.Series)
		}
		reg.RectifiedType = "I" // por diferencia
		lista := make([]xmlalta.RectifiedID, 0, len(b.Rectificadas))
		for _, r := range b.Rectificadas {
			lista = append(lista, xmlalta.RectifiedID{Series: r.Series, Date: r.Date})
		}
		reg.RectifiedInvoices = &lista
	}
	if b.Subsanacion {
		reg.Subsanacion = "S"
	}
	if b.RechazoPrevio {
		reg.PreviousRejection = "S"
	}
	reg.OperationDescription = b.Description
	if b.ClientNIF != "" {
		if _, err := domain.NewNIF(b.ClientNIF); err != nil {
			return nil, fmt.Errorf("NIF del cliente: %v", err)
		}
		reg.Recipients = []xmlalta.Recipient{{Name: b.ClientName, TaxID: b.ClientNIF}}
	}
	reg.Breakdown = lineas
	reg.TotalTax = fromCents(cuotaCent)
	reg.TotalAmount = fromCents(baseCent + cuotaCent)
	reg.Chaining = &encadenamiento
	reg.System = xmlalta.SoftwareSystem{
		Name: sis.NombreProductor, TaxID: sis.NIFProductor,
		SystemName: sis.NombreSistema, SystemID: sis.IDSistema,
		Version: sis.Version, InstallationNumber: sis.NumeroInstalacion,
		VerifactuOnlyUse: "S", MultiTaxpayerUse: "N", MultipleOTIndicator: "N",
	}
	reg.GeneratedAt = generado.ISO8601()
	reg.Fingerprint = huella.Value()
	if err := reg.Validate(); err != nil {
		return nil, fmt.Errorf("el chainRow no cumple el diseño de la AEAT: %v", err)
	}

	sub := xmlalta.NewSubmission(xmlalta.Issuer{Name: emisorNombre, TaxID: nif.Value()}, reg)
	xmlStr, err := sub.XML()
	if err != nil {
		return nil, fmt.Errorf("no se pudo generar el XML de remisión: %v", err)
	}

	res := &SealResult{
		XML: xmlStr, NIF: nif.Value(), Serie: series.Value(),
		Date: fecha.Date(), Tipo: tipo.String(),
		Cuota: fromCents(cuotaCent), Importe: fromCents(baseCent + cuotaCent),
		Generado: generado.ISO8601(), Huella: huella.Value(), EsPrimero: cadena.Last == nil,
	}
	if !res.EsPrimero {
		res.EnlaceSerie = cadena.Last.Serie
		res.EnlaceFecha = cadena.Last.Date
		res.EnlaceHuella = cadena.Last.Huella
	}
	return res, nil
}

func toDomainLink(t ChainLink) *domain.Enlace {
	return &domain.Enlace{
		Serie:  mustSerial(t.Serie),
		Date:   mustDate(t.Date),
		Huella: mustHuella(t.Huella),
	}
}

func toDomainEnlace(t ChainLink) *domain.Enlace { return toDomainLink(t) }

func mustSerial(raw string) domain.SerieNumero {
	s, err := domain.NewSerieNumero(raw)
	if err != nil {
		panic("series inválida ya validada: " + err.Error())
	}
	return s
}

func mustDate(raw string) domain.FechaExpedicion {
	f, err := domain.NewFechaExpedicion(raw)
	if err != nil {
		panic("fecha inválida ya validada: " + err.Error())
	}
	return f
}

func mustHuella(raw string) domain.Huella {
	h, err := domain.NewHuella(raw)
	if err != nil {
		panic("huella inválida: " + err.Error())
	}
	return h
}

func mustTotal(raw string) domain.ImporteTotal {
	v, err := domain.NewImporteTotal(raw)
	if err != nil {
		panic("importe inválido: " + err.Error())
	}
	return v
}

// ToCents parses "123.45" (2 decimals, dot or comma) into exact cents.
func ToCents(raw string) (int, error) { return toCents(raw) }

// FromCents renders exact cents as a decimal string.
func FromCents(c int) string { return fromCents(c) }

func toCents(raw string) (int, error) {
	s := strings.Replace(strings.TrimSpace(raw), ",", ".", 1)
	partes := strings.Split(s, ".")
	if len(partes) > 2 || partes[0] == "" {
		return 0, fmt.Errorf("%q no es un importe válido", raw)
	}
	enteros, err := strconv.Atoi(partes[0])
	if err != nil {
		return 0, fmt.Errorf("%q no es un importe válido", raw)
	}
	dec := 0
	if len(partes) == 2 {
		d := partes[1]
		if len(d) > 2 {
			return 0, fmt.Errorf("%q tiene más de dos decimales", raw)
		}
		for len(d) < 2 {
			d += "0"
		}
		dec, err = strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("%q no es un importe válido", raw)
		}
	}
	// sign applied ONCE at the end (audit fix: -5.25 produced -475 before)
	negativo := strings.HasPrefix(strings.TrimSpace(raw), "-")
	if negativo {
		if enteros < 0 {
			enteros = -enteros
		}
		if dec < 0 {
			dec = -dec
		}
	}
	total := enteros*100 + dec
	if negativo {
		total = -total
	}
	return total, nil
}

func fromCents(c int) string {
	signo := ""
	if c < 0 {
		signo, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", signo, c/100, c%100)
}

func normalizeDecimal(raw string) string {
	return strings.Replace(strings.TrimSpace(raw), ",", ".", 1)
}
