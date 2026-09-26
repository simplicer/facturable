package sello

import (
	"fmt"

	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/domain"
)

// IntegrityReport resume la verificación de la cadena del libro.
type IntegrityReport struct {
	Registros int
	Rupturas  []string
}

// Healthy devuelve true cuando la cadena no tiene rupturas ni huellas inválidas.
func (i IntegrityReport) Healthy() bool { return len(i.Rupturas) == 0 }

// VerifyChain recalcula la huella de CADA chainRow con los datos
// almacenados y comprueba el encadenamiento completo (n→n-1): la
// autocomprobación de integridad del SIF (RRSIF art. 8).
func (s *Service) VerifyChain() (*IntegrityReport, error) {
	registros, err := s.Store.FullChain(s.EmisorNIF)
	if err != nil {
		return nil, err
	}
	info := &IntegrityReport{Registros: len(registros)}

	for idx, r := range registros {
		anterior := ""
		if idx > 0 {
			prev := registros[idx-1]
			if r.EnlaceSerie != prev.Serie || r.EnlaceFecha != prev.Date || r.EnlaceHuella != prev.Huella {
				info.Rupturas = append(info.Rupturas, fmt.Sprintf(
					"chainRow %d (%s): su enlace no apunta al chainRow anterior (%s)",
					r.ID, r.Serie, prev.Serie))
			}
			anterior = prev.Huella
		} else if r.EnlaceSerie != "" {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf(
				"chainRow %d (%s): es el primero pero declara un enlace previo", r.ID, r.Serie))
		}

		var huella domain.Huella
		var err error
		if r.Tipo == "ANULACION" {
			huella, err = recomputeCancellation(s.EmisorNIF, r, anterior)
		} else {
			huella, err = recomputeRegistration(s.EmisorNIF, r, anterior)
		}
		if err != nil {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf("chainRow %d (%s): %v", r.ID, r.Serie, err))
			continue
		}
		if huella.Value() != r.Huella {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf(
				"chainRow %d (%s): huella recalculada %s ≠ almacenada %s",
				r.ID, r.Serie, huella.Value(), r.Huella))
		}
	}
	return info, nil
}

func recomputeRegistration(emisor string, r persistence.ChainRecord, anterior string) (domain.Huella, error) {
	nif, err := domain.NewNIF(emisor)
	if err != nil {
		return domain.Huella{}, err
	}
	series, err := domain.NewSerieNumero(r.Serie)
	if err != nil {
		return domain.Huella{}, err
	}
	fecha, err := domain.NewFechaExpedicion(r.Date)
	if err != nil {
		return domain.Huella{}, err
	}
	tipo, err := domain.NewTipoFactura(r.Tipo)
	if err != nil {
		return domain.Huella{}, err
	}
	cuota, err := domain.NewCuotaTotal(r.Cuota)
	if err != nil {
		return domain.Huella{}, err
	}
	importe, err := domain.NewImporteTotal(r.Importe)
	if err != nil {
		return domain.Huella{}, err
	}
	generado, err := domain.NewFechaHoraHuso(r.Generado)
	if err != nil {
		return domain.Huella{}, err
	}
	ant, err := previousHuella(anterior)
	if err != nil {
		return domain.Huella{}, err
	}
	return domain.HuellaRegistroAlta(domain.RegistroAltaHuella{
		Emisor: nif, Serie: series, Date: fecha, Tipo: tipo,
		Cuota: cuota, Importe: importe, Generado: generado, Previous: ant,
	})
}

// previousHuella admite la cadena vacía del primer chainRow.
func previousHuella(anterior string) (domain.Huella, error) {
	if anterior == "" {
		return domain.Huella{}, nil
	}
	return domain.NewHuella(anterior)
}

func recomputeCancellation(emisor string, r persistence.ChainRecord, anterior string) (domain.Huella, error) {
	nif, err := domain.NewNIF(emisor)
	if err != nil {
		return domain.Huella{}, err
	}
	series, err := domain.NewSerieNumero(r.Serie)
	if err != nil {
		return domain.Huella{}, err
	}
	fecha, err := domain.NewFechaExpedicion(r.Date)
	if err != nil {
		return domain.Huella{}, err
	}
	generado, err := domain.NewFechaHoraHuso(r.Generado)
	if err != nil {
		return domain.Huella{}, err
	}
	ant, err := previousHuella(anterior)
	if err != nil {
		return domain.Huella{}, err
	}
	return domain.HuellaRegistroAnulacion(domain.RegistroAnulacionHuella{
		Emisor: nif, Serie: series, Date: fecha, Generado: generado, Previous: ant,
	})
}
