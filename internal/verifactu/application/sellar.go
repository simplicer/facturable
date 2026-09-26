package application

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"facturable/internal/verifactu/domain"
	"facturable/internal/verifactu/domain/xmlalta"
)

// SistemaLocal identifica el SIF en cada registro (art. 15 OM: productor,
// IdSistemaInformatico de 2 caracteres, versión e instalación).
type SistemaLocal struct {
	NombreProductor, NIFProductor string
	NombreSistema, IDSistema      string // IDSistema: exactamente 2 caracteres
	Version, NumeroInstalacion    string
}

// BorradorAlta es la factura validada por la ingesta, lista para sellarse.
type BorradorAlta struct {
	NumeroSerie, Fecha, Tipo, Descripcion string
	NIFCliente, NombreCliente             string
	Desgloses                             []DesgloseAlta
}

// DesgloseAlta es una línea de IVA del borrador.
type DesgloseAlta struct {
	TipoImpositivo, Base, Cuota string
}

// EstadoCadena es la foto del libro que el llamador saca del almacén:
//   - Ultimo: identidad (serie/fecha/huella) del último registro, n-1.
//   - DeclaradoPorUltimo: el enlace que n-1 incrustó hacia n-2 (nil si n-1
//     fue el primero). Es lo que permite detectar manipulaciones.
//   - Anterior: identidad real de n-2 según el almacén (nil si no existe).
//
// Ultimo nil = cadena vacía.
type EstadoCadena struct {
	Ultimo             *EnlaceTriple
	DeclaradoPorUltimo *EnlaceTriple
	Anterior           *EnlaceTriple
}

// EnlaceTriple identifica un registro: serie, fecha y huella.
type EnlaceTriple struct{ Serie, Fecha, Huella string }

// ResultadoSellar es todo lo que hay que persistir tras sellar.
type ResultadoSellar struct {
	XML string // sobre SOAP completo del registro (envío unitario)

	NIF, Serie, Fecha, Tipo                string
	Cuota, Importe, Generado, Huella       string
	EnlaceSerie, EnlaceFecha, EnlaceHuella string // vacíos si es el primero
	EsPrimero                              bool
}

// SellarAlta convierte un borrador en registro de facturación encadenado
// con su XML de remisión. Verifica el salto n-1/n-2 antes de sellar
// (FAQ AEAT de trazabilidad) y calcula totales por suma exacta en céntimos.
func SellarAlta(emisorNIF, emisorNombre string, b BorradorAlta, sis SistemaLocal, cadena EstadoCadena, ahora time.Time) (*ResultadoSellar, error) {
	nif, err := domain.NewNIF(emisorNIF)
	if err != nil {
		return nil, fmt.Errorf("NIF del emisor: %v", err)
	}
	serie, err := domain.NewSerieNumero(b.NumeroSerie)
	if err != nil {
		return nil, fmt.Errorf("número de factura: %v", err)
	}
	fecha, err := domain.NewFechaExpedicion(b.Fecha)
	if err != nil {
		return nil, fmt.Errorf("fecha: %v", err)
	}
	tipo, err := domain.NewTipoFactura(b.Tipo)
	if err != nil {
		return nil, fmt.Errorf("tipo de factura: %v", err)
	}

	// Totales exactos en céntimos (sin float).
	cuotaCent, baseCent := 0, 0
	lineas := make([]xmlalta.BreakdownLine, 0, len(b.Desgloses))
	for _, d := range b.Desgloses {
		base, err := enCentimos(d.Base)
		if err != nil {
			return nil, fmt.Errorf("base imponible de %s: %v", d.TipoImpositivo, err)
		}
		cuota, err := enCentimos(d.Cuota)
		if err != nil {
			return nil, fmt.Errorf("cuota de %s: %v", d.TipoImpositivo, err)
		}
		baseCent += base
		cuotaCent += cuota
		lineas = append(lineas, xmlalta.BreakdownLine{
			Tax:                    "01", // IVA
			RegimeKey:              "01", // régimen general
			OperationQualification: "S1", // sujeta y no exenta
			TaxRate:                normalizaDecimal(d.TipoImpositivo),
			TaxBase:                desdeCentimos(base),
			TaxFee:                 desdeCentimos(cuota),
		})
	}
	if len(lineas) == 0 {
		return nil, fmt.Errorf("la factura %s no tiene desgloses de IVA", b.NumeroSerie)
	}
	if len(lineas) > 12 {
		return nil, fmt.Errorf("la factura %s supera los 12 desgloses admitidos por la AEAT", b.NumeroSerie)
	}

	// Verificación del salto de cadena antes de generar la huella.
	var huellaAnterior domain.Huella
	var encadenamiento xmlalta.Chaining
	switch {
	case cadena.Ultimo == nil:
		encadenamiento = xmlalta.Chaining{FirstRecord: "S"}
	default:
		ult := tripleADominio(*cadena.Ultimo)
		var declarado, ant *domain.Enlace
		if cadena.DeclaradoPorUltimo != nil {
			declarado = tripleAEnlace(*cadena.DeclaradoPorUltimo)
		}
		if cadena.Anterior != nil {
			ant = tripleAEnlace(*cadena.Anterior)
		}
		vigente := domain.RegistroVigente{Enlace: *ult, Referencia: declarado}
		if err := domain.VerificarSalto(vigente, ant); err != nil {
			return nil, fmt.Errorf("no se puede sellar: %v", err)
		}
		huellaAnterior = ult.Huella
		encadenamiento = xmlalta.Chaining{PreviousRecord: &xmlalta.PreviousRecord{
			IssuerID:       nif.Value(),
			Number:         ult.Serie.Value(),
			ExpeditionDate: ult.Fecha.Fecha(),
			Fingerprint:    ult.Huella.Value(),
		}}
	}

	generado := domain.NewFechaHoraHusoFromTime(ahora)
	huella, err := domain.HuellaRegistroAlta(domain.RegistroAltaHuella{
		Emisor: nif, Serie: serie, Fecha: fecha, Tipo: tipo,
		Cuota:    mustImporteTotal(desdeCentimos(cuotaCent)),
		Importe:  mustImporteTotal(desdeCentimos(baseCent + cuotaCent)),
		Generado: generado, Anterior: huellaAnterior,
	})
	if err != nil {
		return nil, err
	}

	reg := xmlalta.NewRegistrationRecord()
	reg.InvoiceID = xmlalta.InvoiceID{IssuerID: nif.Value(), Number: serie.Value(), ExpeditionDate: fecha.Fecha()}
	reg.IssuerName = emisorNombre
	reg.InvoiceType = tipo.String()
	reg.OperationDescription = b.Descripcion
	if b.NIFCliente != "" {
		if _, err := domain.NewNIF(b.NIFCliente); err != nil {
			return nil, fmt.Errorf("NIF del cliente: %v", err)
		}
		reg.Recipients = []xmlalta.Recipient{{Name: b.NombreCliente, TaxID: b.NIFCliente}}
	}
	reg.Breakdown = lineas
	reg.TotalTax = desdeCentimos(cuotaCent)
	reg.TotalAmount = desdeCentimos(baseCent + cuotaCent)
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
		return nil, fmt.Errorf("el registro no cumple el diseño de la AEAT: %v", err)
	}

	sub := xmlalta.NewSubmission(xmlalta.Issuer{Name: emisorNombre, TaxID: nif.Value()}, reg)
	xmlStr, err := sub.XML()
	if err != nil {
		return nil, fmt.Errorf("no se pudo generar el XML de remisión: %v", err)
	}

	res := &ResultadoSellar{
		XML: xmlStr, NIF: nif.Value(), Serie: serie.Value(),
		Fecha: fecha.Fecha(), Tipo: tipo.String(),
		Cuota: desdeCentimos(cuotaCent), Importe: desdeCentimos(baseCent + cuotaCent),
		Generado: generado.ISO8601(), Huella: huella.Value(), EsPrimero: cadena.Ultimo == nil,
	}
	if !res.EsPrimero {
		res.EnlaceSerie = cadena.Ultimo.Serie
		res.EnlaceFecha = cadena.Ultimo.Fecha
		res.EnlaceHuella = cadena.Ultimo.Huella
	}
	return res, nil
}

func tripleADominio(t EnlaceTriple) *domain.Enlace {
	return &domain.Enlace{
		Serie:  mustSerieVO(t.Serie),
		Fecha:  mustFechaVO(t.Fecha),
		Huella: mustHuellaVO(t.Huella),
	}
}

func tripleAEnlace(t EnlaceTriple) *domain.Enlace { return tripleADominio(t) }

func mustSerieVO(raw string) domain.SerieNumero {
	s, err := domain.NewSerieNumero(raw)
	if err != nil {
		panic("serie inválida ya validada: " + err.Error())
	}
	return s
}

func mustFechaVO(raw string) domain.FechaExpedicion {
	f, err := domain.NewFechaExpedicion(raw)
	if err != nil {
		panic("fecha inválida ya validada: " + err.Error())
	}
	return f
}

func mustHuellaVO(raw string) domain.Huella {
	h, err := domain.NewHuella(raw)
	if err != nil {
		panic("huella inválida: " + err.Error())
	}
	return h
}

func mustImporteTotal(raw string) domain.ImporteTotal {
	v, err := domain.NewImporteTotal(raw)
	if err != nil {
		panic("importe inválido: " + err.Error())
	}
	return v
}

// enCentimos parsea "123.45" (2 decimales, punto o coma) a céntimos exactos.
func enCentimos(raw string) (int, error) {
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
	negativo := enteros < 0
	if negativo {
		enteros, dec = -enteros, -dec
	}
	total := enteros*100 + dec
	if negativo {
		total = -total
	}
	return total, nil
}

func desdeCentimos(c int) string {
	signo := ""
	if c < 0 {
		signo, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", signo, c/100, c%100)
}

func normalizaDecimal(raw string) string {
	return strings.Replace(strings.TrimSpace(raw), ",", ".", 1)
}
