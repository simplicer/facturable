package declaration

import (
	"strings"
	"testing"
)

func TestTextContieneDatosObligatorios(t *testing.T) {
	txt := Text(Info{
		ProducerName: "Facturable SL", ProducerNIF: "B72877814",
		ProducerContact: "soporte@facturable.es",
		SystemName:      "Facturable Sello", SystemID: "FS",
		Version: "0.1.0-unstable", Installation: "00001",
		OnlyVerifactu: true, MultiOT: false,
	})
	for _, trozo := range []string{
		"DECLARACIÓN RESPONSABLE", "Facturable SL", "B72877814",
		"Facturable Sello", "FS", "0.1.0-unstable", "00001",
		"TipoUsoPosibleSoloVerifactu: S", "29.2.j", "VERI*FACTU",
	} {
		if !strings.Contains(txt, trozo) {
			t.Fatalf("la declaración no contiene %q", trozo)
		}
	}
}
