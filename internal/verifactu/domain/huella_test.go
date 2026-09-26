package domain

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Anchors from the official AEAT sources.
const (
	vectorGITHUB   = "4EECCE4DD48C0539665385D61D451BA921B7160CA6FEF46CD3C2E2BC5C778E14"
	vectorCaso1    = "3C464DAF61ACB827C65FDA19F352A4E3BDC2C640E9E9FC4CC058073F38F12F60"
	vectorCaso2    = "F7B94CFD8924EDFF273501B01EE5153E4CE8F259766F88CF6ACB8935802A2B97"
	vectorCaso3    = "177547C0D57AC74748561D054A9CEC14B4C4EA23D1BEFD6F2E69E3A388F90C68"
	previousHuella = "8C8DCEFB120522E0C71BC19902F44D5334FF6C98E74F0E3AC1D1E5A30C2EA836"
)

func altaFixture(t *testing.T) RegistroAltaHuella {
	t.Helper()
	return RegistroAltaHuella{
		Emisor:   mustNIF(t, "B72877814"),
		Serie:    mustSerie(t, "GITHUB-EJ-003"),
		Date:     mustFecha(t, "04-11-2024"),
		Tipo:     FacturaCompleta,
		Cuota:    mustImporte(t, "21.4"),
		Importe:  mustImporte(t, "131.4"),
		Generado: mustHuso(t, "2024-11-04T12:36:39+01:00"),
		Previous: mustHuella(t, previousHuella),
	}
}

// TestHuellaRegistroAltaAEATVector pins the official example from the AEAT
// fingerprint examples (mdiago/VeriFactu doc, section 1). It must pass
// bit for bit.
func TestHuellaRegistroAltaAEATVector(t *testing.T) {
	got, err := HuellaRegistroAlta(altaFixture(t))
	if err != nil {
		t.Fatalf("HuellaRegistroAlta unexpected error: %v", err)
	}
	if got.Value() != vectorGITHUB {
		t.Fatalf("huella =\n%s\nwant bit-for-bit:\n%s", got.Value(), vectorGITHUB)
	}
}

// TestHuellaRegistroAltaOfficialCaso1 covers the "first record" case from the
// official AEAT huella specification v0.1.2 (page 10): Huella= (empty).
func TestHuellaRegistroAltaOfficialCaso1(t *testing.T) {
	input := RegistroAltaHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345678/G33"),
		Date:     mustFecha(t, "01-01-2024"),
		Tipo:     FacturaCompleta,
		Cuota:    mustImporte(t, "12.35"),
		Importe:  mustImporte(t, "123.45"),
		Generado: mustHuso(t, "2024-01-01T19:20:30+01:00"),
	}
	got, err := HuellaRegistroAlta(input)
	if err != nil {
		t.Fatalf("HuellaRegistroAlta unexpected error: %v", err)
	}
	if got.Value() != vectorCaso1 {
		t.Fatalf("huella =\n%s\nwant bit-for-bit:\n%s", got.Value(), vectorCaso1)
	}
}

// TestHuellaRegistroAltaOfficialCaso2 covers the "second record" case from the
// official AEAT huella specification v0.1.2 (page 11).
func TestHuellaRegistroAltaOfficialCaso2(t *testing.T) {
	input := RegistroAltaHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345679/G34"),
		Date:     mustFecha(t, "01-01-2024"),
		Tipo:     FacturaCompleta,
		Cuota:    mustImporte(t, "12.35"),
		Importe:  mustImporte(t, "123.45"),
		Generado: mustHuso(t, "2024-01-01T19:20:35+01:00"),
		Previous: mustHuella(t, vectorCaso1),
	}
	got, err := HuellaRegistroAlta(input)
	if err != nil {
		t.Fatalf("HuellaRegistroAlta unexpected error: %v", err)
	}
	if got.Value() != vectorCaso2 {
		t.Fatalf("huella =\n%s\nwant bit-for-bit:\n%s", got.Value(), vectorCaso2)
	}
}

// TestHuellaRegistroAnulacionOfficialCaso3 covers the cancellation case from
// the official AEAT huella specification v0.1.2 (page 12).
func TestHuellaRegistroAnulacionOfficialCaso3(t *testing.T) {
	input := RegistroAnulacionHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345679/G34"),
		Date:     mustFecha(t, "01-01-2024"),
		Generado: mustHuso(t, "2024-01-01T19:20:40+01:00"),
		Previous: mustHuella(t, vectorCaso2),
	}
	got, err := HuellaRegistroAnulacion(input)
	if err != nil {
		t.Fatalf("HuellaRegistroAnulacion unexpected error: %v", err)
	}
	if got.Value() != vectorCaso3 {
		t.Fatalf("huella =\n%s\nwant bit-for-bit:\n%s", got.Value(), vectorCaso3)
	}
}

// TestAltaHuellaInputExactString pins the exact concatenation format:
// key=value joined by "&", Huella before FechaHoraHusoGenRegistro (art. 137.a
// of the Orden HAC/1177/2024; AEAT huella spec v0.1.2, pages 5-7).
func TestAltaHuellaInputExactString(t *testing.T) {
	want := "IDEmisorFactura=B72877814" +
		"&NumSerieFactura=GITHUB-EJ-003" +
		"&FechaExpedicionFactura=04-11-2024" +
		"&TipoFactura=F1" +
		"&CuotaTotal=21.4" +
		"&ImporteTotal=131.4" +
		"&Huella=" + previousHuella +
		"&FechaHoraHusoGenRegistro=2024-11-04T12:36:39+01:00"
	got := altaHuellaInput(altaFixture(t))
	if got != want {
		t.Fatalf("hash input =\n%s\nwant:\n%s", got, want)
	}
}

// TestAnulacionHuellaInputExactString pins the 5-field cancellation format
// (art. 137.b of the Orden HAC/1177/2024): no TipoFactura, no amounts, and
// Anulada-suffixed identifiers.
func TestAnulacionHuellaInputExactString(t *testing.T) {
	input := RegistroAnulacionHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345679/G34"),
		Date:     mustFecha(t, "01-01-2024"),
		Generado: mustHuso(t, "2024-01-01T19:20:40+01:00"),
		Previous: mustHuella(t, vectorCaso2),
	}
	want := "IDEmisorFacturaAnulada=89890001K" +
		"&NumSerieFacturaAnulada=12345679/G34" +
		"&FechaExpedicionFacturaAnulada=01-01-2024" +
		"&Huella=" + vectorCaso2 +
		"&FechaHoraHusoGenRegistro=2024-01-01T19:20:40+01:00"
	got := anulacionHuellaInput(input)
	if got != want {
		t.Fatalf("hash input =\n%s\nwant:\n%s", got, want)
	}
}

func TestHuellaOutputFormat(t *testing.T) {
	got, err := HuellaRegistroAlta(altaFixture(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Value()) != 64 {
		t.Fatalf("huella length = %d, want 64", len(got.Value()))
	}
	if !regexp.MustCompile(`^[0-9A-F]{64}$`).MatchString(got.Value()) {
		t.Fatalf("huella %q is not 64 uppercase hex chars", got.Value())
	}
}

func TestHuellaRegistroAltaRejectsIncompleteRecords(t *testing.T) {
	base := altaFixture(t)
	tests := []struct {
		name   string
		mutate func(*RegistroAltaHuella)
	}{
		{name: "missing emisor", mutate: func(r *RegistroAltaHuella) { r.Emisor = NIF{} }},
		{name: "missing series", mutate: func(r *RegistroAltaHuella) { r.Serie = SerieNumero{} }},
		{name: "missing fecha", mutate: func(r *RegistroAltaHuella) { r.Date = FechaExpedicion{} }},
		{name: "missing tipo", mutate: func(r *RegistroAltaHuella) { r.Tipo = "" }},
		{name: "missing cuota", mutate: func(r *RegistroAltaHuella) { r.Cuota = CuotaTotal{} }},
		{name: "missing importe", mutate: func(r *RegistroAltaHuella) { r.Importe = ImporteTotal{} }},
		{name: "missing generation instant", mutate: func(r *RegistroAltaHuella) { r.Generado = FechaHoraHuso{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := base
			tt.mutate(&record)
			if _, err := HuellaRegistroAlta(record); err == nil {
				t.Fatal("expected error for incomplete record, got none")
			}
		})
	}
}

func TestHuellaIsDeterministic(t *testing.T) {
	first, err := HuellaRegistroAlta(altaFixture(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := HuellaRegistroAlta(altaFixture(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Value() != second.Value() {
		t.Fatalf("same input produced different huellas: %q vs %q", first.Value(), second.Value())
	}
}

// TestHuellaIsSensitiveToEveryField is a property: changing any single field
// of the record changes the fingerprint.
func TestHuellaIsSensitiveToEveryField(t *testing.T) {
	base := altaFixture(t)
	baseline, err := HuellaRegistroAlta(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RegistroAltaHuella)
	}{
		{name: "emisor", mutate: func(r *RegistroAltaHuella) { r.Emisor = mustNIF(t, "B44531218") }},
		{name: "series", mutate: func(r *RegistroAltaHuella) { r.Serie = mustSerie(t, "GITHUB-EJ-004") }},
		{name: "fecha", mutate: func(r *RegistroAltaHuella) { r.Date = mustFecha(t, "05-11-2024") }},
		{name: "tipo", mutate: func(r *RegistroAltaHuella) { r.Tipo = FacturaSimplificada }},
		{name: "cuota", mutate: func(r *RegistroAltaHuella) { r.Cuota = mustImporte(t, "21.5") }},
		{name: "importe", mutate: func(r *RegistroAltaHuella) { r.Importe = mustImporte(t, "131.5") }},
		{name: "generado", mutate: func(r *RegistroAltaHuella) { r.Generado = mustHuso(t, "2024-11-04T12:36:40+01:00") }},
		{name: "anterior", mutate: func(r *RegistroAltaHuella) { r.Previous = mustHuella(t, vectorCaso1) }},
		{name: "anterior cleared", mutate: func(r *RegistroAltaHuella) { r.Previous = Huella{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := base
			tt.mutate(&record)
			got, err := HuellaRegistroAlta(record)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Value() == baseline.Value() {
				t.Fatalf("huella unchanged after mutating %s", tt.name)
			}
		})
	}
}

// TestAnulacionHuellaSensitiveToFields mirrors the sensitivity property for
// cancellations.
func TestAnulacionHuellaSensitiveToFields(t *testing.T) {
	base := RegistroAnulacionHuella{
		Emisor:   mustNIF(t, "89890001K"),
		Serie:    mustSerie(t, "12345679/G34"),
		Date:     mustFecha(t, "01-01-2024"),
		Generado: mustHuso(t, "2024-01-01T19:20:40+01:00"),
		Previous: mustHuella(t, vectorCaso2),
	}
	baseline, err := HuellaRegistroAnulacion(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mutations := map[string]func(*RegistroAnulacionHuella){
		"emisor":   func(r *RegistroAnulacionHuella) { r.Emisor = mustNIF(t, "B72877814") },
		"series":   func(r *RegistroAnulacionHuella) { r.Serie = mustSerie(t, "12345678/G33") },
		"fecha":    func(r *RegistroAnulacionHuella) { r.Date = mustFecha(t, "02-01-2024") },
		"generado": func(r *RegistroAnulacionHuella) { r.Generado = mustHuso(t, "2024-01-01T19:20:41+01:00") },
		"anterior": func(r *RegistroAnulacionHuella) { r.Previous = Huella{} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			record := base
			mutate(&record)
			got, err := HuellaRegistroAnulacion(record)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Value() == baseline.Value() {
				t.Fatalf("anulacion huella unchanged after mutating %s", name)
			}
		})
	}
}

// TestHuellaChainOfN is the property test requested by the plan: for a chain
// of n records, huella_i must embed huella_{i-1}, so the chain is verifiable
// record by record and any tampering is detected from record i-1 onwards.
func TestHuellaChainOfN(t *testing.T) {
	const total = 5
	huellas := make([]Huella, total)
	records := make([]RegistroAltaHuella, total)
	for i := 0; i < total; i++ {
		record := RegistroAltaHuella{
			Emisor:   mustNIF(t, "B72877814"),
			Serie:    mustSerie(t, "TEST-2024-"+strconv.Itoa(i+1)),
			Date:     mustFecha(t, "01-01-2024"),
			Tipo:     FacturaCompleta,
			Cuota:    mustImporte(t, "12.35"),
			Importe:  mustImporte(t, "123.45"),
			Generado: mustHuso(t, mustInstant(i)),
		}
		if i > 0 {
			record.Previous = huellas[i-1]
		}
		huella, err := HuellaRegistroAlta(record)
		if err != nil {
			t.Fatalf("record %d: unexpected error: %v", i, err)
		}
		records[i] = record
		huellas[i] = huella
	}

	// Property 1: every huella_i can be recomputed from its own fields plus
	// huella_{i-1}; verification succeeds along the whole chain.
	for i := 0; i < total; i++ {
		recomputed, err := HuellaRegistroAlta(records[i])
		if err != nil {
			t.Fatalf("record %d: unexpected error: %v", i, err)
		}
		if recomputed.Value() != huellas[i].Value() {
			t.Fatalf("record %d: chain verification failed", i)
		}
	}

	// Property 2: tampering with record 0 changes every huella of the chain,
	// because a real chain is built sequentially over the tampered record.
	tampered := records
	tampered[0].Importe = mustImporte(t, "123.46")
	var tamperedHuellas []Huella
	for i := 0; i < total; i++ {
		if i > 0 {
			tampered[i].Previous = tamperedHuellas[i-1]
		}
		got, err := HuellaRegistroAlta(tampered[i])
		if err != nil {
			t.Fatalf("record %d: unexpected error: %v", i, err)
		}
		tamperedHuellas = append(tamperedHuellas, got)
		if got.Value() == huellas[i].Value() {
			t.Fatalf("record %d: tampering with record 0 was not detected", i)
		}
	}

	// Property 3: altas and anulaciones share one chain: an anulacion chained
	// to the last alta differs from the alta chained to itself.
	anulacion := RegistroAnulacionHuella{
		Emisor:   mustNIF(t, "B72877814"),
		Serie:    mustSerie(t, "TEST-2024-001"),
		Date:     mustFecha(t, "01-01-2024"),
		Generado: mustHuso(t, mustInstant(total)),
		Previous: huellas[total-1],
	}
	chainWithAnulacion, err := HuellaRegistroAnulacion(anulacion)
	if err != nil {
		t.Fatalf("anulacion: unexpected error: %v", err)
	}
	if chainWithAnulacion.Value() == huellas[total-1].Value() {
		t.Fatal("anulacion huella collided with the alta huella it chains to")
	}
}

func TestNewHuellaValidation(t *testing.T) {
	if _, err := NewHuella("deadc0de"); err == nil {
		t.Fatal("expected error for short huella, got none")
	}
	if _, err := NewHuella(strings.ToLower(vectorCaso1)); err == nil {
		t.Fatal("expected error for lowercase huella, got none")
	}
	if _, err := NewHuella(strings.Repeat("G", 64)); err == nil {
		t.Fatal("expected error for non-hex huella, got none")
	}
	got, err := NewHuella(vectorCaso1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Value() != vectorCaso1 {
		t.Fatalf("Value() = %q, want %q", got.Value(), vectorCaso1)
	}
}

func mustNIF(t *testing.T, raw string) NIF {
	t.Helper()
	value, err := NewNIF(raw)
	if err != nil {
		t.Fatalf("NewNIF(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustSerie(t *testing.T, raw string) SerieNumero {
	t.Helper()
	value, err := NewSerieNumero(raw)
	if err != nil {
		t.Fatalf("NewSerieNumero(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustFecha(t *testing.T, raw string) FechaExpedicion {
	t.Helper()
	value, err := NewFechaExpedicion(raw)
	if err != nil {
		t.Fatalf("NewFechaExpedicion(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustImporte(t *testing.T, raw string) Importe {
	t.Helper()
	value, err := NewImporte(raw)
	if err != nil {
		t.Fatalf("NewImporte(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustHuso(t *testing.T, raw string) FechaHoraHuso {
	t.Helper()
	value, err := NewFechaHoraHuso(raw)
	if err != nil {
		t.Fatalf("NewFechaHoraHuso(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustHuella(t *testing.T, raw string) Huella {
	t.Helper()
	value, err := NewHuella(raw)
	if err != nil {
		t.Fatalf("NewHuella(%q) unexpected error: %v", raw, err)
	}
	return value
}

func mustInstant(index int) string {
	// +01:00 (peninsular Spain) so the instants look like the AEAT examples.
	zone := time.FixedZone("+01:00", 3600)
	return time.Date(2024, 1, 1, 19, 20, 30+index, 0, zone).Format(time.RFC3339)
}
