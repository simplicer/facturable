// Package qr builds and validates the AEAT «URL de cotejo» (verification URL)
// encoded in the Veri*Factu QR code of an invoice record.
//
// The URL never carries the fingerprint (huella): per AEAT, the QR only
// allows cotejo by NIF, invoice number, date and total amount
// (see docs/NORMATIVA.md).
package qr

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Environment is the AEAT environment the QR URL points to.
type Environment int

const (
	// Production is the real AEAT environment.
	Production Environment = iota
	// Testing is the AEAT sandbox («pruebas»).
	Testing
)

// String implements fmt.Stringer with English identifiers.
func (e Environment) String() string {
	switch e {
	case Production:
		return "production"
	case Testing:
		return "testing"
	default:
		return fmt.Sprintf("environment(%d)", int(e))
	}
}

const (
	productionBase = "https://www2.agenciatributaria.gob.es" + validationPath
	testingBase    = "https://prewww2.aeat.es" + validationPath

	validationPath = "/wlpl/TIKE-CONT/ValidarQR"
)

// Query holds the four fields the AEAT QR verification endpoint accepts.
type Query struct {
	NIF            string // issuer NIF (nif)
	InvoiceNumber  string // NumSerieFactura (numserie)
	ExpeditionDate string // DD-MM-AAAA (fecha)
	TotalAmount    string // ImporteTotal, dot decimal, e.g. 131.4 (importe)
}

var (
	datePattern    = regexp.MustCompile(`^\d{2}-\d{2}-\d{4}$`)
	amountPattern  = regexp.MustCompile(`^\d+([.,]\d{1,2})?$`)
	queryParamKeys = []string{"nif", "numserie", "fecha", "importe"}
)

// VerificationURL builds the AEAT cotejo URL for the given environment and
// query. It returns an error (in Spanish) when the query is not valid.
func VerificationURL(env Environment, q Query) (string, error) {
	var base string
	switch env {
	case Production:
		base = productionBase
	case Testing:
		base = testingBase
	default:
		return "", fmt.Errorf("entorno de cotejo desconocido: %d", int(env))
	}

	if err := q.Validate(); err != nil {
		return "", err
	}
	amount, err := normalizeAmount(q.TotalAmount)
	if err != nil {
		return "", err
	}

	// Parameter order matches the official example (nif, numserie, fecha,
	// importe) so URLs are byte-for-byte reproducible.
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("?nif=")
	b.WriteString(url.QueryEscape(q.NIF))
	b.WriteString("&numserie=")
	b.WriteString(url.QueryEscape(q.InvoiceNumber))
	b.WriteString("&fecha=")
	b.WriteString(url.QueryEscape(q.ExpeditionDate))
	b.WriteString("&importe=")
	b.WriteString(url.QueryEscape(amount))
	return b.String(), nil
}

// Validate checks the query fields and returns a Spanish error describing the
// first problem found.
func (q Query) Validate() error {
	if q.NIF == "" {
		return errors.New("el NIF del emisor es obligatorio en la URL de cotejo")
	}
	if q.InvoiceNumber == "" {
		return errors.New("el número de series de la factura es obligatorio en la URL de cotejo")
	}
	if !validDate(q.ExpeditionDate) {
		return fmt.Errorf("la fecha de expedición debe tener formato DD-MM-AAAA: %q", q.ExpeditionDate)
	}
	if _, err := normalizeAmount(q.TotalAmount); err != nil {
		return err
	}
	return nil
}

// ParseVerificationURL validates a raw cotejo URL and extracts its environment
// and query. The returned amount is normalized to dot decimal without trailing
// zeros, so that VerificationURL(env, q) reproduces the input byte for byte.
func ParseVerificationURL(rawurl string) (Environment, Query, error) {
	parsed, err := url.Parse(rawurl)
	if err != nil {
		return Production, Query{}, fmt.Errorf("la URL de cotejo no es válida: %v", err)
	}

	if parsed.Scheme != "https" {
		return Production, Query{}, fmt.Errorf("la URL de cotejo debe usar https: %q", parsed.Scheme)
	}

	var env Environment
	switch parsed.Host {
	case "www2.agenciatributaria.gob.es":
		env = Production
	case "prewww2.aeat.es":
		env = Testing
	default:
		return Production, Query{}, fmt.Errorf("el host %q no corresponde a ningún entorno de cotejo (producción o pruebas)", parsed.Host)
	}

	if parsed.Path != validationPath {
		return Production, Query{}, fmt.Errorf("la ruta de la URL de cotejo debe ser %s: %q", validationPath, parsed.Path)
	}

	values := parsed.Query()

	// The QR carries exactly four fields; anything else (e.g. a fingerprint)
	// makes the URL invalid: the official FAQ forbids the huella in the QR.
	for _, key := range queryParamKeys {
		if values.Get(key) == "" {
			return Production, Query{}, fmt.Errorf("a la URL de cotejo le falta el parámetro %q", key)
		}
	}
	if extra := len(values) - len(queryParamKeys); extra > 0 {
		return Production, Query{}, fmt.Errorf("la URL de cotejo no admite parámetros adicionales a %s (sin huella): %d de más", strings.Join(queryParamKeys, ", "), extra)
	}

	q := Query{
		NIF:            values.Get("nif"),
		InvoiceNumber:  values.Get("numserie"),
		ExpeditionDate: values.Get("fecha"),
	}
	amount, err := normalizeAmount(values.Get("importe"))
	if err != nil {
		return Production, Query{}, err
	}
	q.TotalAmount = amount

	if err := q.Validate(); err != nil {
		return Production, Query{}, err
	}
	return env, q, nil
}

// validDate reports whether the value is a real calendar date in DD-MM-AAAA.
func validDate(value string) bool {
	if !datePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("02-01-2006", value)
	return err == nil
}

// normalizeAmount converts a decimal amount with comma or dot into the AEAT QR
// format: dot decimal separator without trailing zeros ("131,40" → "131.4",
// "20.00" → "20"). Amounts are handled as strings, never as floats.
func normalizeAmount(amount string) (string, error) {
	trimmed := strings.TrimSpace(amount)
	if !amountPattern.MatchString(trimmed) {
		return "", fmt.Errorf("el importe debe ser un número decimal positivo, p. ej. 131.4: %q", amount)
	}
	normalized := strings.Replace(trimmed, ",", ".", 1)
	whole, decimals, _ := strings.Cut(normalized, ".")
	decimals = strings.TrimRight(decimals, "0")
	if decimals == "" {
		return whole, nil
	}
	return whole + "." + decimals, nil
}
