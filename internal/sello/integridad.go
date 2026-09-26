package sello

import (
	"fmt"

	"facturable/internal/sello/persistencia"
	"facturable/internal/verifactu/domain"
)

// InformeIntegridad resume la verificación de la cadena del libro.
type InformeIntegridad struct {
	Registros int
	Rupturas  []string
}

// Sana devuelve true cuando la cadena no tiene rupturas ni huellas inválidas.
func (i InformeIntegridad) Sana() bool { return len(i.Rupturas) == 0 }

// VerificarCadena recalcula la huella de CADA registro con los datos
// almacenados y comprueba el encadenamiento completo (n→n-1): la
// autocomprobación de integridad del SIF (RRSIF art. 8).
func (s *Servicio) VerificarCadena() (*InformeIntegridad, error) {
	registros, err := s.Store.CadenaCompleta(s.EmisorNIF)
	if err != nil {
		return nil, err
	}
	info := &InformeIntegridad{Registros: len(registros)}

	for idx, r := range registros {
		anterior := ""
		if idx > 0 {
			prev := registros[idx-1]
			if r.EnlaceSerie != prev.Serie || r.EnlaceFecha != prev.Fecha || r.EnlaceHuella != prev.Huella {
				info.Rupturas = append(info.Rupturas, fmt.Sprintf(
					"registro %d (%s): su enlace no apunta al registro anterior (%s)",
					r.ID, r.Serie, prev.Serie))
			}
			anterior = prev.Huella
		} else if r.EnlaceSerie != "" {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf(
				"registro %d (%s): es el primero pero declara un enlace previo", r.ID, r.Serie))
		}

		var huella domain.Huella
		var err error
		if r.Tipo == "ANULACION" {
			huella, err = recalcularAnulacion(s.EmisorNIF, r, anterior)
		} else {
			huella, err = recalcularAlta(s.EmisorNIF, r, anterior)
		}
		if err != nil {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf("registro %d (%s): %v", r.ID, r.Serie, err))
			continue
		}
		if huella.Value() != r.Huella {
			info.Rupturas = append(info.Rupturas, fmt.Sprintf(
				"registro %d (%s): huella recalculada %s ≠ almacenada %s",
				r.ID, r.Serie, huella.Value(), r.Huella))
		}
	}
	return info, nil
}

func recalcularAlta(emisor string, r persistencia.RegistroCadena, anterior string) (domain.Huella, error) {
	nif, err := domain.NewNIF(emisor)
	if err != nil {
		return domain.Huella{}, err
	}
	serie, err := domain.NewSerieNumero(r.Serie)
	if err != nil {
		return domain.Huella{}, err
	}
	fecha, err := domain.NewFechaExpedicion(r.Fecha)
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
	ant, err := huellaAnterior(anterior)
	if err != nil {
		return domain.Huella{}, err
	}
	return domain.HuellaRegistroAlta(domain.RegistroAltaHuella{
		Emisor: nif, Serie: serie, Fecha: fecha, Tipo: tipo,
		Cuota: cuota, Importe: importe, Generado: generado, Anterior: ant,
	})
}

// huellaAnterior admite la cadena vacía del primer registro.
func huellaAnterior(anterior string) (domain.Huella, error) {
	if anterior == "" {
		return domain.Huella{}, nil
	}
	return domain.NewHuella(anterior)
}

func recalcularAnulacion(emisor string, r persistencia.RegistroCadena, anterior string) (domain.Huella, error) {
	nif, err := domain.NewNIF(emisor)
	if err != nil {
		return domain.Huella{}, err
	}
	serie, err := domain.NewSerieNumero(r.Serie)
	if err != nil {
		return domain.Huella{}, err
	}
	fecha, err := domain.NewFechaExpedicion(r.Fecha)
	if err != nil {
		return domain.Huella{}, err
	}
	generado, err := domain.NewFechaHoraHuso(r.Generado)
	if err != nil {
		return domain.Huella{}, err
	}
	ant, err := huellaAnterior(anterior)
	if err != nil {
		return domain.Huella{}, err
	}
	return domain.HuellaRegistroAnulacion(domain.RegistroAnulacionHuella{
		Emisor: nif, Serie: serie, Fecha: fecha, Generado: generado, Anterior: ant,
	})
}
