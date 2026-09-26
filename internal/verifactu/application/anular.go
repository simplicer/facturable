package application

import (
	"fmt"
	"time"

	"facturable/internal/verifactu/domain"
	"facturable/internal/verifactu/domain/xmlalta"
)

// SellarAnulacion genera el registro de anulación de una factura emitida y
// lo encadena como cualquier otro registro (FAQ AEAT: altas y anulaciones
// comparten la MISMA cadena, en orden de generación).
func SellarAnulacion(emisorNIF string, serie, fecha string, sis SistemaLocal, cadena EstadoCadena, ahora time.Time) (*ResultadoSellar, error) {
	nif, err := domain.NewNIF(emisorNIF)
	if err != nil {
		return nil, fmt.Errorf("NIF del emisor: %v", err)
	}
	serieVO, err := domain.NewSerieNumero(serie)
	if err != nil {
		return nil, fmt.Errorf("número de la factura a anular: %v", err)
	}
	fechaVO, err := domain.NewFechaExpedicion(fecha)
	if err != nil {
		return nil, fmt.Errorf("fecha de la factura a anular: %v", err)
	}
	if cadena.Ultimo == nil {
		return nil, fmt.Errorf("no se puede anular %s: el libro no tiene registros de esa empresa", serie)
	}

	// verificación del salto n-1/n-2 (idéntica a SellarAlta)
	ult := tripleADominio(*cadena.Ultimo)
	var declarado, ant *domain.Enlace
	if cadena.DeclaradoPorUltimo != nil {
		declarado = tripleAEnlace(*cadena.DeclaradoPorUltimo)
	}
	if cadena.Anterior != nil {
		ant = tripleAEnlace(*cadena.Anterior)
	}
	if err := domain.VerificarSalto(domain.RegistroVigente{Enlace: *ult, Referencia: declarado}, ant); err != nil {
		return nil, fmt.Errorf("no se puede anular: %v", err)
	}

	generado := domain.NewFechaHoraHusoFromTime(ahora)
	huella, err := domain.HuellaRegistroAnulacion(domain.RegistroAnulacionHuella{
		Emisor: nif, Serie: serieVO, Fecha: fechaVO, Generado: generado, Anterior: ult.Huella,
	})
	if err != nil {
		return nil, err
	}

	reg := xmlalta.NewCancellationRecord()
	reg.InvoiceID = xmlalta.CancelledInvoiceID{
		IssuerID: nif.Value(), Number: serieVO.Value(), ExpeditionDate: fechaVO.Fecha(),
	}
	reg.Chaining = &xmlalta.Chaining{PreviousRecord: &xmlalta.PreviousRecord{
		IssuerID: nif.Value(), Number: ult.Serie.Value(),
		ExpeditionDate: ult.Fecha.Fecha(), Fingerprint: ult.Huella.Value(),
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
		return nil, fmt.Errorf("el registro de anulación no cumple el diseño AEAT: %v", err)
	}

	sub := xmlalta.NewSubmission(xmlalta.Issuer{Name: sis.NombreProductor, TaxID: nif.Value()}, reg)
	xmlStr, err := sub.XML()
	if err != nil {
		return nil, fmt.Errorf("no se pudo generar el XML de anulación: %v", err)
	}

	return &ResultadoSellar{
		XML: xmlStr, NIF: nif.Value(), Serie: serieVO.Value(),
		Fecha: fechaVO.Fecha(), Tipo: "ANULACION",
		Generado: generado.ISO8601(), Huella: huella.Value(),
		EnlaceSerie: cadena.Ultimo.Serie, EnlaceFecha: cadena.Ultimo.Fecha,
		EnlaceHuella: cadena.Ultimo.Huella,
	}, nil
}
