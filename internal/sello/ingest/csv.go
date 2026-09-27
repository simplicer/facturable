// Package ingest turns customer CSV files into validated invoice drafts.
// The documented template is one row per VAT breakdown; rows sharing
// numero_serie group into one invoice (column names are end-user facing
// Spanish by design).
//
//	Required header:
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

// templateColumns of the documented CSV template (name matching is
// case/separator insensitive).
var templateColumns = []string{"numero_serie", "fecha", "tipo", "descripcion", "nif_cliente", "nombre_cliente", "tipo_impositivo", "base", "cuota"}

// BreakdownDraft is one VAT line of a draft.
type BreakdownDraft struct {
	TaxRate string `json:"tipo_impositivo"` // e.g. "21"
	Base    string `json:"base"`            // "100.00"
	Fee     string `json:"cuota"`           // "21.00"
}

// Draft is a validated invoice ready to stamp: the JSON wire format for
// the relay/API ingestion and the internal shape of grouped CSV rows.
type Draft struct {
	Series      string           `json:"numero_serie"`
	Date        string           `json:"fecha"` // DD-MM-AAAA
	Tipo        string           `json:"tipo"`  // F1, R1…
	Description string           `json:"descripcion"`
	ClientNIF   string           `json:"nif_cliente,omitempty"`
	ClientName  string           `json:"nombre_cliente,omitempty"`
	Breakdowns  []BreakdownDraft `json:"desgloses"`

	Fila             int  `json:"-"` // first CSV row where it appeared
	brokenBreakdowns bool `json:"-"` // some breakdown row already errored
}

// RowError localizes a data problem in the source CSV row.
type RowError struct {
	Fila    int    `json:"fila"`
	Campo   string `json:"campo"`
	Detalle string `json:"detalle"`
}

func (e RowError) Error() string {
	return fmt.Sprintf("fila %d, campo %s: %s", e.Fila, e.Campo, e.Detalle)
}

// ReadCSV validates and groups a whole CSV. It returns the valid drafts and
// the per-row errors (one bad row does not abort the file).
func ReadCSV(r io.Reader) (drafts []Draft, errores []RowError) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err == io.EOF {
		return nil, []RowError{{Fila: 1, Campo: "cabecera", Detalle: "el fichero está vacío"}}
	}
	if err != nil {
		return nil, []RowError{{Fila: 1, Campo: "cabecera", Detalle: "no se pudo leer el CSV: " + err.Error()}}
	}
	index, err := columnIndex(header)
	if err != nil {
		return nil, []RowError{{Fila: 1, Campo: "cabecera", Detalle: err.Error()}}
	}

	bySeries := map[string]*Draft{}
	orderSeen := []string{}

	rowNum := 1
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			errores = append(errores, RowError{Fila: rowNum, Campo: "fila", Detalle: err.Error()})
			continue
		}
		if isEmptyRow(row) {
			continue
		}
		if len(row) != len(templateColumns) {
			errores = append(errores, RowError{Fila: rowNum, Campo: "fila",
				Detalle: fmt.Sprintf("se esperaban %d columnas y hay %d (usa la plantilla)", len(templateColumns), len(row))})
			continue
		}

		series := strings.TrimSpace(row[index["numero_serie"]])
		if d, ok := bySeries[series]; ok {
			// same series: every row must repeat identical header fields,
			// otherwise the silent merge would alter the invoice
			if d.Date != strings.TrimSpace(row[index["fecha"]]) ||
				d.Tipo != strings.ToUpper(strings.TrimSpace(row[index["tipo"]])) ||
				d.ClientNIF != strings.TrimSpace(row[index["nif_cliente"]]) ||
				d.Description != strings.TrimSpace(row[index["descripcion"]]) ||
				d.ClientName != strings.TrimSpace(row[index["nombre_cliente"]]) {
				errores = append(errores, RowError{Fila: rowNum, Campo: "numero_serie",
					Detalle: fmt.Sprintf("la serie %s ya apareció con otra fecha, tipo o cliente (audit fix: no se fusiona en silencio)", series)})
				continue
			}
		}
		d, ok := bySeries[series]
		if !ok {
			d = &Draft{
				Series:      series,
				Date:        strings.TrimSpace(row[index["fecha"]]),
				Tipo:        strings.ToUpper(strings.TrimSpace(row[index["tipo"]])),
				Description: strings.TrimSpace(row[index["descripcion"]]),
				ClientNIF:   strings.TrimSpace(row[index["nif_cliente"]]),
				ClientName:  strings.TrimSpace(row[index["nombre_cliente"]]),
				Fila:        rowNum,
			}
			if e := validateRowHeader(d, rowNum); e != nil {
				errores = append(errores, *e)
				continue // invalid header row: the draft is not kept
			}
			bySeries[series] = d
			orderSeen = append(orderSeen, series)
		}

		b := BreakdownDraft{
			TaxRate: strings.TrimSpace(row[index["tipo_impositivo"]]),
			Base:    strings.TrimSpace(row[index["base"]]),
			Fee:     strings.TrimSpace(row[index["cuota"]]),
		}
		if err := validateBreakdown(b); err != nil {
			errores = append(errores, RowError{Fila: rowNum, Campo: "desglose", Detalle: err.Error()})
			d.brokenBreakdowns = true
			continue
		}
		d.Breakdowns = append(d.Breakdowns, b)
	}

	sort.Slice(orderSeen, func(i, j int) bool { return bySeries[orderSeen[i]].Fila < bySeries[orderSeen[j]].Fila })
	for _, s := range orderSeen {
		d := bySeries[s]
		if len(d.Breakdowns) == 0 {
			if !d.brokenBreakdowns {
				errores = append(errores, RowError{Fila: d.Fila, Campo: "desglose",
					Detalle: fmt.Sprintf("la factura %s no tiene ningún desglose de IVA", s)})
			}
			continue
		}
		drafts = append(drafts, *d)
	}
	return drafts, errores
}

func validateRowHeader(d *Draft, rowNum int) *RowError {
	if _, err := domain.NewSerieNumero(d.Series); err != nil {
		return &RowError{Fila: rowNum, Campo: "numero_serie", Detalle: err.Error()}
	}
	if _, err := domain.NewFechaExpedicion(d.Date); err != nil {
		return &RowError{Fila: rowNum, Campo: "fecha", Detalle: err.Error()}
	}
	if _, err := domain.NewTipoFactura(d.Tipo); err != nil {
		return &RowError{Fila: rowNum, Campo: "tipo", Detalle: err.Error()}
	}
	if d.ClientNIF != "" {
		if _, err := domain.NewNIF(d.ClientNIF); err != nil {
			return &RowError{Fila: rowNum, Campo: "nif_cliente", Detalle: err.Error()}
		}
	}
	return nil
}

func validateBreakdown(b BreakdownDraft) error {
	tipo := strings.Replace(b.TaxRate, ",", ".", 1)
	if _, err := strconv.ParseFloat(tipo, 64); err != nil {
		return fmt.Errorf("tipo impositivo %q no es un número", b.TaxRate)
	}
	if _, err := domain.NewImporte(b.Base); err != nil {
		return fmt.Errorf("base: %v", err)
	}
	if _, err := domain.NewImporte(b.Fee); err != nil {
		return fmt.Errorf("cuota: %v", err)
	}
	return nil
}

func columnIndex(header []string) (map[string]int, error) {
	out := map[string]int{}
	for i, c := range header {
		out[norm(c)] = i
	}
	for _, want := range templateColumns {
		if _, ok := out[want]; !ok {
			return nil, fmt.Errorf("falta la columna %q en la cabecera (plantilla: %s)", want, strings.Join(templateColumns, ","))
		}
	}
	return out, nil
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return strings.ReplaceAll(s, "-", "_")
}

func isEmptyRow(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
