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
