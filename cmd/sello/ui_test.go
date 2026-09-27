package main

import (
	"strings"
	"testing"
)

// TestEmbeddedUICertificateWizard guards onboarding step 2 (certificate
// wizard) and the final first-invoice step: the embedded UI must keep the
// FNMT guidance, the install flow, the help contact and the final call to
// stamp the first invoice.
func TestEmbeddedUICertificateWizard(t *testing.T) {
	for _, want := range []string{
		"https://www.cert.fnmt.es",
		"Solicita el certificado de persona jurídica en la FNMT",
		"Descarga el archivo .p12 que te envían",
		"Súbelo aquí:",
		"Instalar certificado",
		"✓ Certificado instalado",
		"Probar conexión",
		"¿Necesitas ayuda? Escríbenos y te acompañamos por videollamada",
		"hola@facturable.es",
		"✓ Todo listo. Ahora arrastra tu primera factura PDF y séllela.",
	} {
		if !strings.Contains(formUI, want) {
			t.Errorf("embedded UI is missing %q", want)
		}
	}
}

// TestEmbeddedUIOCRFallbackMessage: when the extraction falls back to OCR
// and tesseract is not installed, the user must get the clear "install
// tesseract or fill the fields manually" instruction instead of being
// blocked by a raw error (audit UX).
func TestEmbeddedUIOCRFallbackMessage(t *testing.T) {
	for _, want := range []string{
		"Tu factura es una imagen escaneada",
		"Instala el programa gratuito Tesseract OCR",
		"https://github.com/tesseract-ocr/tesseract",
		"O rellena los campos manualmente abajo",
	} {
		if !strings.Contains(formUI, want) {
			t.Errorf("embedded UI is missing %q", want)
		}
	}
}

// TestEmbeddedUIStampInProgressGuard: the stamp flow must flag the in-flight
// stamping in sessionStorage before the fetch, so a user who closes the tab
// while "Sellando…" spins is warned on the next load that the record may
// already exist (audit UX).
func TestEmbeddedUIStampInProgressGuard(t *testing.T) {
	for _, want := range []string{
		"sessionStorage.setItem",
		"facturable-stamp-in-progress",
		"La última operación pudo no completarse. Revisa el libro de facturas.",
	} {
		if !strings.Contains(formUI, want) {
			t.Errorf("embedded UI is missing %q", want)
		}
	}
}

// TestEmbeddedUILibroStatusLabels: the invoice book table must show the AEAT
// states in plain Spanish with colors instead of the raw enum values the
// user cannot interpret (audit UX).
func TestEmbeddedUILibroStatusLabels(t *testing.T) {
	for _, want := range []string{
		"Esperando envío",
		"Enviado a Hacienda ✓",
		"Aceptada con avisos",
		"Rechazada — necesita corrección",
		"estado-pendiente",
		"estado-correcto",
		"estado-avisos",
		"estado-rechazado",
	} {
		if !strings.Contains(formUI, want) {
			t.Errorf("embedded UI is missing %q", want)
		}
	}
}

// TestEmbeddedUILibroUsesEstadoFilter: the book table must read the records
// collection through the ?estado= query param so the filter really queries,
// not just repaints (audit UX).
func TestEmbeddedUILibroUsesEstadoFilter(t *testing.T) {
	for _, want := range []string{
		"/v1/records",
		"estado=",
	} {
		if !strings.Contains(formUI, want) {
			t.Errorf("embedded UI is missing %q", want)
		}
	}
}
