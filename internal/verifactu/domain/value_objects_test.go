package domain

import "testing"

func TestValueObjectStringMethods(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "nif", got: mustNIF(t, "12345678Z").String(), want: "12345678Z"},
		{name: "series", got: mustSerie(t, "A-001").String(), want: "A-001"},
		{name: "fecha", got: mustFecha(t, "04-11-2024").String(), want: "04-11-2024"},
		{name: "importe", got: mustImporte(t, "21.4").String(), want: "21.40"},
		{name: "huso", got: mustHuso(t, "2024-11-04T12:36:39+01:00").String(), want: "2024-11-04T12:36:39+01:00"},
		{name: "huella", got: mustHuella(t, vectorCaso1).String(), want: vectorCaso1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("String() = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestHuellaIsZeroForUnchainedRecords(t *testing.T) {
	if !(Huella{}).IsZero() {
		t.Fatal("zero Huella must report IsZero")
	}
	if mustHuella(t, vectorCaso1).IsZero() {
		t.Fatal("real huella must not report IsZero")
	}
}

func TestTipoFacturaIsRectificativa(t *testing.T) {
	rectificativas := []TipoFactura{Rectificativa1, Rectificativa2, Rectificativa3, Rectificativa4, Rectificativa5}
	for _, tipo := range rectificativas {
		if !tipo.IsRectificativa() {
			t.Fatalf("%q should be rectificativa", tipo)
		}
	}
	facturas := []TipoFactura{FacturaCompleta, FacturaSimplificada, FacturaSustitutiva, FacturaPorErrorFundado}
	for _, tipo := range facturas {
		if tipo.IsRectificativa() {
			t.Fatalf("%q should not be rectificativa", tipo)
		}
	}
}

func TestHuellaRegistroAnulacionRejectsIncompleteRecords(t *testing.T) {
	base := RegistroAnulacionHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345679/G34"),
		Date:     mustFecha(t, "01-01-2024"),
		Generado: mustHuso(t, "2024-01-01T19:20:40+01:00"),
	}
	tests := []struct {
		name   string
		mutate func(*RegistroAnulacionHuella)
	}{
		{name: "missing emisor", mutate: func(r *RegistroAnulacionHuella) { r.Emisor = NIF{} }},
		{name: "missing series", mutate: func(r *RegistroAnulacionHuella) { r.Serie = SerieNumero{} }},
		{name: "missing fecha", mutate: func(r *RegistroAnulacionHuella) { r.Date = FechaExpedicion{} }},
		{name: "missing generation instant", mutate: func(r *RegistroAnulacionHuella) { r.Generado = FechaHoraHuso{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := base
			tt.mutate(&record)
			if _, err := HuellaRegistroAnulacion(record); err == nil {
				t.Fatalf("%s: expected error, got none", tt.name)
			}
		})
	}
}

func TestNewNIFRejectsWrongLetterControlForLetterRequiredCIF(t *testing.T) {
	// P4620000 has control digit 2, whose letter form is B; D is rejected.
	if _, err := NewNIF("P4620000D"); err == nil {
		t.Fatal("expected error for wrong letter control on letter-required CIF, got none")
	}
}
