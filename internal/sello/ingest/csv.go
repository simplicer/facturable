// Package ingest turns customer CSV files into validated invoice drafts.
// The documented template is one row per VAT breakdown; rows sharing
// numero_serie group into one invoice (column names are end-user facing
// Spanish by design).
//
//	Cabecera obligatoria:
//	numero_serie,fecha,tipo,descripcion,nif_cliente,nombre_cliente,tipo_impositivo,base,cuota
package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"facturable/internal/verifactu/domain"
)

// Columnas de la plantilla (kebab/snake insensible).
var columnas = []string{"numero_serie", "fecha", "tipo", "descripcion", "nif_cliente", "nombre_cliente", "tipo_impositivo", "base", "cuota"}

// DesgloseBorrador es una línea de IVA de la factura.
type DesgloseBorrador struct {
	TipoImpositivo string // p.ej. "21"
	Base           string // "100.00"
	Cuota          string // "21.00"
}

// Borrador es una factura lista para sellar.
type Borrador struct {
	NumeroSerie   string
	Fecha         string // DD-MM-AAAA
	Tipo          string // F1, R1…
	Descripcion   string
	NIFCliente    string // vacío permitido en simplificadas
	NombreCliente string
	Desgloses     []DesgloseBorrador
	Fila          int // primera fila del CSV donde apareció

	desglosesFallidos bool // alguna fila de desglose suya ya reportó error
}

// ErrorFila localiza un problem de datos en el CSV original.
type ErrorFila struct {
	Fila    int
	Campo   string
	Detalle string
}

func (e ErrorFila) Error() string {
	return fmt.Sprintf("fila %d, campo %s: %s", e.Fila, e.Campo, e.Detalle)
}

// LeerCSV valida y agrupa un CSV completo. Devuelve los borradores válidos
// y los errores por fila (no aborta todo el fichero por una fila mala).
func LeerCSV(r io.Reader) (borradores []Borrador, errores []ErrorFila) {
	lectura := csv.NewReader(r)
	lectura.FieldsPerRecord = -1

	cabecera, err := lectura.Read()
	if err == io.EOF {
		return nil, []ErrorFila{{Fila: 1, Campo: "cabecera", Detalle: "el fichero está vacío"}}
	}
	if err != nil {
		return nil, []ErrorFila{{Fila: 1, Campo: "cabecera", Detalle: "no se pudo leer el CSV: " + err.Error()}}
	}
	indice, err := columnIndex(cabecera)
	if err != nil {
		return nil, []ErrorFila{{Fila: 1, Campo: "cabecera", Detalle: err.Error()}}
	}

	porSerie := map[string]*Borrador{}
	orden := []string{}

	numFila := 1
	for {
		fila, err := lectura.Read()
		if err == io.EOF {
			break
		}
		numFila++
		if err != nil {
			errores = append(errores, ErrorFila{Fila: numFila, Campo: "fila", Detalle: err.Error()})
			continue
		}
		if emptyRow(fila) {
			continue
		}
		if len(fila) != len(columnas) {
			errores = append(errores, ErrorFila{Fila: numFila, Campo: "fila",
				Detalle: fmt.Sprintf("se esperaban %d columnas y hay %d (usa la plantilla)", len(columnas), len(fila))})
			continue
		}

		serie := strings.TrimSpace(fila[indice["numero_serie"]])
		b, ok := porSerie[serie]
		if !ok {
			b = &Borrador{
				NumeroSerie:   serie,
				Fecha:         strings.TrimSpace(fila[indice["fecha"]]),
				Tipo:          strings.ToUpper(strings.TrimSpace(fila[indice["tipo"]])),
				Descripcion:   strings.TrimSpace(fila[indice["descripcion"]]),
				NIFCliente:    strings.TrimSpace(fila[indice["nif_cliente"]]),
				NombreCliente: strings.TrimSpace(fila[indice["nombre_cliente"]]),
				Fila:          numFila,
			}
			if e := validateRowHeader(b, numFila); e != nil {
				errores = append(errores, *e)
				continue // cabecera inválida: el borrador no entra
			}
			porSerie[serie] = b
			orden = append(orden, serie)
		}

		d := DesgloseBorrador{
			TipoImpositivo: strings.TrimSpace(fila[indice["tipo_impositivo"]]),
			Base:           strings.TrimSpace(fila[indice["base"]]),
			Cuota:          strings.TrimSpace(fila[indice["cuota"]]),
		}
		if err := validateBreakdown(d); err != nil {
			errores = append(errores, ErrorFila{Fila: numFila, Campo: "desglose", Detalle: err.Error()})
			b.desglosesFallidos = true
			continue
		}
		b.Desgloses = append(b.Desgloses, d)
	}

	sort.Slice(orden, func(i, j int) bool { return porSerie[orden[i]].Fila < porSerie[orden[j]].Fila })
	for _, s := range orden {
		b := porSerie[s]
		if len(b.Desgloses) == 0 {
			if !b.desglosesFallidos {
				errores = append(errores, ErrorFila{Fila: b.Fila, Campo: "desglose",
					Detalle: fmt.Sprintf("la factura %s no tiene ningún desglose de IVA", s)})
			}
			continue
		}
		borradores = append(borradores, *b)
	}
	return borradores, errores
}

func validateRowHeader(b *Borrador, numFila int) *ErrorFila {
	if _, err := domain.NewSerieNumero(b.NumeroSerie); err != nil {
		return &ErrorFila{Fila: numFila, Campo: "numero_serie", Detalle: err.Error()}
	}
	if _, err := domain.NewFechaExpedicion(b.Fecha); err != nil {
		return &ErrorFila{Fila: numFila, Campo: "fecha", Detalle: err.Error()}
	}
	if _, err := domain.NewTipoFactura(b.Tipo); err != nil {
		return &ErrorFila{Fila: numFila, Campo: "tipo", Detalle: err.Error()}
	}
	if b.NIFCliente != "" {
		if _, err := domain.NewNIF(b.NIFCliente); err != nil {
			return &ErrorFila{Fila: numFila, Campo: "nif_cliente", Detalle: err.Error()}
		}
	}
	return nil
}

func validateBreakdown(d DesgloseBorrador) error {
	tipo := strings.Replace(d.TipoImpositivo, ",", ".", 1)
	if _, err := strconv.ParseFloat(tipo, 64); err != nil {
		return fmt.Errorf("tipo impositivo %q no es un número", d.TipoImpositivo)
	}
	if _, err := domain.NewImporte(d.Base); err != nil {
		return fmt.Errorf("base: %v", err)
	}
	if _, err := domain.NewImporte(d.Cuota); err != nil {
		return fmt.Errorf("cuota: %v", err)
	}
	return nil
}

func columnIndex(cabecera []string) (map[string]int, error) {
	out := map[string]int{}
	for i, c := range cabecera {
		out[norm(c)] = i
	}
	for _, want := range columnas {
		if _, ok := out[want]; !ok {
			return nil, fmt.Errorf("falta la columna %q en la cabecera (plantilla: %s)", want, strings.Join(columnas, ","))
		}
	}
	return out, nil
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func emptyRow(fila []string) bool {
	for _, c := range fila {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
