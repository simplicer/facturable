package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// huellaPattern is the required output shape: hexadecimal, uppercase, 64
// characters (especificaciones huella AEAT v0.1.2, page 9).
var huellaPattern = regexp.MustCompile(`^[0-9A-F]{64}$`)

// Huella is the SHA-256 fingerprint of a billing record, rendered as 64
// uppercase hexadecimal characters. The zero value represents "no previous
// record" and renders as an empty Huella= field in the hash input.
type Huella struct {
	value string
}

// NewHuella validates a fingerprint coming from storage or the wire. The
// AEAT issues and expects uppercase hexadecimal, so lowercase is rejected
// instead of silently rewritten (the chain must compare like for like).
func NewHuella(raw string) (Huella, error) {
	value := trimSpaces(raw)
	if !huellaPattern.MatchString(value) {
		return Huella{}, fmt.Errorf(
			"la huella %q no es válida (se requieren 64 caracteres hexadecimales en mayúsculas)", raw)
	}
	return Huella{value: value}, nil
}

// Value returns the uppercase hexadecimal fingerprint.
func (h Huella) Value() string {
	return h.value
}

// IsZero reports whether there is no previous record in the chain.
func (h Huella) IsZero() bool {
	return h.value == ""
}

func (h Huella) String() string {
	return h.value
}

// RegistroAltaHuella gathers the fields the AEAT hashes for a RegistroAlta
// (art. 137.a of the Orden HAC/1177/2024; huella spec v0.1.2, page 5).
type RegistroAltaHuella struct {
	Emisor   NIF             // IDEmisorFactura
	Serie    SerieNumero     // NumSerieFactura
	Date     FechaExpedicion // FechaExpedicionFactura
	Tipo     TipoFactura     // TipoFactura
	Cuota    CuotaTotal      // CuotaTotal
	Importe  ImporteTotal    // ImporteTotal
	Generado FechaHoraHuso   // FechaHoraHusoGenRegistro
	Previous Huella          // Encadenamiento/RegistroAnterior/Huella (zero for the first record)
}

// HuellaRegistroAlta computes the fingerprint of a billing registration
// record. It returns an error when the record is incomplete so a malformed
// record can never enter the chain.
func HuellaRegistroAlta(record RegistroAltaHuella) (Huella, error) {
	if record.Emisor.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta el NIF del emisor")
	}
	if record.Serie.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta el número de factura")
	}
	if record.Date.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta la fecha de expedición")
	}
	if record.Tipo == "" {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta el tipo de factura")
	}
	if record.Cuota.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta la cuota total")
	}
	if record.Importe.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta el importe total")
	}
	if record.Generado.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella: falta la fecha y hora de generación")
	}
	return newHuellaFromInput(altaHuellaInput(record)), nil
}

// RegistroAnulacionHuella gathers the fields the AEAT hashes for a
// RegistroAnulacion (art. 137.b of the Orden HAC/1177/2024; huella spec
// v0.1.2, page 5): no invoice type, no amounts, Anulada-suffixed names.
type RegistroAnulacionHuella struct {
	Emisor   NIF             // IDEmisorFacturaAnulada
	Serie    SerieNumero     // NumSerieFacturaAnulada
	Date     FechaExpedicion // FechaExpedicionFacturaAnulada
	Generado FechaHoraHuso   // FechaHoraHusoGenRegistro
	Previous Huella          // Encadenamiento/RegistroAnterior/Huella (zero for the first record)
}

// HuellaRegistroAnulacion computes the fingerprint of a cancellation record.
func HuellaRegistroAnulacion(record RegistroAnulacionHuella) (Huella, error) {
	if record.Emisor.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella de anulación: falta el NIF del emisor")
	}
	if record.Serie.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella de anulación: falta el número de factura")
	}
	if record.Date.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella de anulación: falta la fecha de expedición")
	}
	if record.Generado.IsZero() {
		return Huella{}, fmt.Errorf("no se puede calcular la huella de anulación: falta la fecha y hora de generación")
	}
	return newHuellaFromInput(anulacionHuellaInput(record)), nil
}

// altaHuellaInput builds the exact hash input string:
// nombreCampo1=valorCampo1&nombreCampo2=valorCampo2&... with Huella before
// FechaHoraHusoGenRegistro (huella spec v0.1.2, pages 6-7).
func altaHuellaInput(record RegistroAltaHuella) string {
	return strings.Join([]string{
		"IDEmisorFactura=" + record.Emisor.Value(),
		"NumSerieFactura=" + record.Serie.Value(),
		"FechaExpedicionFactura=" + record.Date.Date(),
		"TipoFactura=" + record.Tipo.String(),
		"CuotaTotal=" + record.Cuota.WithoutTrailingZeros(),
		"ImporteTotal=" + record.Importe.WithoutTrailingZeros(),
		"Huella=" + record.Previous.Value(),
		"FechaHoraHusoGenRegistro=" + record.Generado.ISO8601(),
	}, "&")
}

// anulacionHuellaInput builds the exact hash input string for a
// cancellation record (huella spec v0.1.2, pages 5 and 12).
func anulacionHuellaInput(record RegistroAnulacionHuella) string {
	return strings.Join([]string{
		"IDEmisorFacturaAnulada=" + record.Emisor.Value(),
		"NumSerieFacturaAnulada=" + record.Serie.Value(),
		"FechaExpedicionFacturaAnulada=" + record.Date.Date(),
		"Huella=" + record.Previous.Value(),
		"FechaHoraHusoGenRegistro=" + record.Generado.ISO8601(),
	}, "&")
}

// newHuellaFromInput hashes the UTF-8 input and renders it uppercase.
func newHuellaFromInput(input string) Huella {
	digest := sha256.Sum256([]byte(input))
	return Huella{value: strings.ToUpper(hex.EncodeToString(digest[:]))}
}
