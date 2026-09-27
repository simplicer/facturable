package pdf

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// facturaPDF builds a minimal single-page invoice PDF shaped like the ones
// the customer's invoicing programs produce (valid enough for the Stamp
// parser, which appends the QR overlay to the first content stream).
func facturaPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n")
	b.WriteString("2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n")
	b.WriteString("3 0 obj << /Type /Page /Parent 2 0 R /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n")
	b.WriteString("4 0 obj << /Length 60 >> stream\nBT /F1 12 Tf 40 700 Td (FACTURA F-2027-0001) Tj ET\nendstream endobj\n")
	b.WriteString("5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n")
	b.WriteString("trailer << /Size 6 /Root 1 0 R >>\n%%EOF")
	return b.Bytes()
}

// facturaProtegida is the same invoice carrying the /Encrypt dictionary
// reference in the trailer: a password-protected document the user cannot
// modify, so stamping is impossible and must be refused with a clear
// instruction instead of a cryptic parser error.
func facturaProtegida() []byte {
	return bytes.Replace(facturaPDF(),
		[]byte("trailer << /Size 6 /Root 1 0 R >>"),
		[]byte("trailer << /Size 6 /Root 1 0 R /Encrypt 6 0 R >>"), 1)
}

// TestStampDetectaPDFProtegido: an encrypted PDF (/Encrypt in the trailer)
// must be refused with the user-facing password message, not a cryptic
// stream error (audit UX).
func TestStampDetectaPDFProtegido(t *testing.T) {
	_, err := Stamp(StampRequest{
		OriginalPDF: facturaProtegida(), Serie: "F-2027-0001", Fecha: "05-01-2027",
		ClienteNIF: "B44531218", ClienteNombre: "ACME SL",
		BaseImponible: "1000.00", CuotaIVA: "210.00", TipoIVA: "21",
		URLCotejo: "https://example.com/qr",
	})
	if err == nil {
		t.Fatal("stamping a password-protected PDF must fail")
	}
	if !errors.Is(err, ErrPasswordProtected) {
		t.Fatalf("error = %v, want ErrPasswordProtected", err)
	}
	for _, want := range []string{
		"protegida con contraseña",
		"Quítala desde tu programa de facturación y vuelve a intentarlo",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q: %v", want, err)
		}
	}
}

// TestStampPDFLibreEstampa: the same invoice WITHOUT /Encrypt must stamp
// normally (the guard must not false-positive on regular documents).
func TestStampPDFLibreEstampa(t *testing.T) {
	stamped, err := Stamp(StampRequest{
		OriginalPDF: facturaPDF(), Serie: "F-2027-0001", Fecha: "05-01-2027",
		ClienteNIF: "B44531218", ClienteNombre: "ACME SL",
		BaseImponible: "1000.00", CuotaIVA: "210.00", TipoIVA: "21",
		URLCotejo: "https://example.com/qr",
	})
	if err != nil {
		t.Fatalf("Stamp(): %v", err)
	}
	if !bytes.Contains(stamped, []byte("VERI*FACTU")) {
		t.Fatal("the stamped PDF lost the VERI*FACTU legend")
	}
}
