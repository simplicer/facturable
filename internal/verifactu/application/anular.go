package application

import (
	"fmt"
	"time"

	"facturable/internal/verifactu/domain"
	"facturable/internal/verifactu/domain/xmlalta"
)

// SellarAnulacion genera el chainRow de anulación de una factura emitida y
// lo encadena como cualquier otro chainRow (FAQ AEAT: altas y anulaciones
// comparten la MISMA cadena, en orderSeen de generación).
func SellarAnulacion(emisorNIF, emisorNombre string, series, fecha string, sis SistemaLocal, cadena EstadoCadena, ahora time.Time) (*ResultadoSellar, error) {
	nif, err := domain.NewNIF(emisorNIF)
	if err != nil {
		return nil, fmt.Errorf("NIF del emisor: %v", err)
	}
	serieVO, err := domain.NewSerieNumero(series)
	if err != nil {
		return nil, fmt.Errorf("número de la factura a anular: %v", err)
	}
	fechaVO, err := domain.NewFechaExpedicion(fecha)
	if err != nil {
		return nil, fmt.Errorf("fecha de la factura a anular: %v", err)
	}
	if cadena.Last == nil {
		return nil, fmt.Errorf("no se puede anular %s: el libro no tiene registros de esa empresa", series)
	}

	// verificación del salto n-1/n-2 (idéntica a SellarAlta)
	ult := toDomainLink(*cadena.Last)
	var declarado, ant *domain.Enlace
	if cadena.DeclaradoPorUltimo != nil {
		declarado = toDomainEnlace(*cadena.DeclaradoPorUltimo)
	}
	if cadena.Previous != nil {
		ant = toDomainEnlace(*cadena.Previous)
	}
	if err := domain.VerificarSalto(domain.RegistroVigente{Enlace: *ult, Referencia: declarado}, ant); err != nil {
		return nil, fmt.Errorf("no se puede anular: %v", err)
	}

	generado := domain.NewFechaHoraHusoFromTime(ahora)
	huella, err := domain.HuellaRegistroAnulacion(domain.RegistroAnulacionHuella{
		Emisor: nif, Serie: serieVO, Date: fechaVO, Generado: generado, Previous: ult.Huella,
	})
	if err != nil {
		return nil, err
	}

	reg := xmlalta.NewCancellationRecord()
	reg.InvoiceID = xmlalta.CancelledInvoiceID{
		IssuerID: nif.Value(), Number: serieVO.Value(), ExpeditionDate: fechaVO.Date(),
	}
	reg.Chaining = &xmlalta.Chaining{PreviousRecord: &xmlalta.PreviousRecord{
		IssuerID: nif.Value(), Number: ult.Serie.Value(),
		ExpeditionDate: ult.Date.Date(), Fingerprint: ult.Huella.Value(),
	}}
	reg.System = xmlalta.SoftwareSystem{
		Name: sis.NombreProductor, TaxID: sis.NIFProductor,
		SystemName: sis.NombreSistema, SystemID: sis.IDSistema,
		Version: sis.Version, InstallationNumber: sis.NumeroInstalacion,
		VerifactuOnlyUse: "S", MultiTaxpayerUse: "N", MultipleOTIndicator: "N",
	}
	reg.GeneratedAt = generado.ISO8601()
	reg.Fingerprint = huella.Value()
	if err := reg.Validate(); err != nil {
		return nil, fmt.Errorf("el chainRow de anulación no cumple el diseño AEAT: %v", err)
	}

	// ObligadoEmision is the invoice ISSUER (the taxpayer), not the software
	// producer: mixing them breaks the AEAT census match (audit finding).
	sub := xmlalta.NewSubmission(xmlalta.Issuer{Name: emisorNombre, TaxID: nif.Value()}, reg)
	xmlStr, err := sub.XML()
	if err != nil {
		return nil, fmt.Errorf("no se pudo generar el XML de anulación: %v", err)
	}

	return &ResultadoSellar{
		XML: xmlStr, NIF: nif.Value(), Serie: serieVO.Value(),
		Date: fechaVO.Date(), Tipo: "ANULACION",
		Generado: generado.ISO8601(), Huella: huella.Value(),
		EnlaceSerie: cadena.Last.Serie, EnlaceFecha: cadena.Last.Date,
		EnlaceHuella: cadena.Last.Huella,
	}, nil
}
