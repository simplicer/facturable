package sello

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"time"

	"facturable/internal/sello/persistencia"
)

// ExportarLibro produce el CSV del periodo para la gestoría (libro de
// facturas emitidas con su estado AEAT). Disponible SIEMPRE, con o sin
// licencia vigente: el usuario nunca es rehén de sus datos.
func ExportarLibro(filas []persistencia.FilaListado) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	cabecera := []string{"Serie", "Fecha", "Tipo", "Base+Cuota", "CuotaTotal",
		"ImporteTotal", "EstadoAEAT", "CSV", "Huella", "Remitido"}
	if err := w.Write(cabecera); err != nil {
		return nil, err
	}
	for _, f := range filas {
		if err := w.Write([]string{
			f.Serie, f.Fecha, f.Tipo, fmt.Sprintf("%s|%s", f.Cuota, f.Importe),
			f.Cuota, f.Importe, f.Estado, f.CSV, f.Huella, f.Remitido,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// ExportarPeriodo vuelca el libro de un rango de fechas (DD-MM-AAAA).
func (s *Servicio) ExportarPeriodo(desde, hasta string) ([]byte, int, error) {
	d, err := time.Parse("02-01-2006", desde)
	if err != nil {
		return nil, 0, fmt.Errorf("fecha inicial %q no válida (usa DD-MM-AAAA): %v", desde, err)
	}
	h, err := time.Parse("02-01-2006", hasta)
	if err != nil {
		return nil, 0, fmt.Errorf("fecha final %q no válida (usa DD-MM-AAAA): %v", hasta, err)
	}
	if h.Before(d) {
		return nil, 0, fmt.Errorf("la fecha final (%s) es anterior a la inicial (%s)", hasta, desde)
	}
	filas, err := s.Store.Listado(s.EmisorNIF, 0, 1_000_000_000, 1_000_000)
	if err != nil {
		return nil, 0, err
	}
	seleccion := filtrarPeriodo(filas, d, h)
	b, err := ExportarLibro(seleccion)
	return b, len(seleccion), err
}

func filtrarPeriodo(filas []persistencia.FilaListado, desde, hasta time.Time) []persistencia.FilaListado {
	var out []persistencia.FilaListado
	for _, f := range filas {
		fecha, err := time.Parse("02-01-2006", f.Fecha)
		if err != nil {
			continue
		}
		if !fecha.Before(desde) && !fecha.After(hasta) {
			out = append(out, f)
		}
	}
	return out
}
