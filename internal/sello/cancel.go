package sello

import (
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// Cancel genera y guarda el chainRow de anulación de una factura.
func (s *Service) Cancel(series, fecha string) (int64, error) {
	estado, err := s.chainState()
	if err != nil {
		return 0, err
	}
	res, err := application.SealCancellation(s.EmisorNIF, s.EmisorNombre, series, fecha, s.Sistema, estado, s.clock())
	if err != nil {
		return 0, err
	}
	id, err := s.Store.Append(persistence.StoredRecord{
		NIF: res.NIF, Serie: res.Serie, Date: res.Date, Tipo: res.Tipo,
		Generado: res.Generado, Cuota: "0.00", Importe: "0.00",
		Huella: res.Huella, XML: res.XML,
		EnlaceSerie: res.EnlaceSerie, EnlaceFecha: res.EnlaceFecha, EnlaceHuella: res.EnlaceHuella,
	})
	return id, err
}
