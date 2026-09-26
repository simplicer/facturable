package qr

import (
	"strings"
	"testing"
)

// Spec case (verified source: /tmp/vf/mdiago_VeriFactu.md:347):
// https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4
func TestVerificationURLSpecCase(t *testing.T) {
	got, err := VerificationURL(Testing, Query{
		NIF:            "B72877814",
		InvoiceNumber:  "GITHUB-EJ-004",
		ExpeditionDate: "04-11-2024",
		TotalAmount:    "131.4",
	})
	if err != nil {
		t.Fatalf("VerificationURL() error = %v", err)
	}
	want := "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4"
	if got != want {
		t.Fatalf("VerificationURL() =\n%s\nwant byte-for-byte:\n%s", got, want)
	}
}

func TestVerificationURLProductionBase(t *testing.T) {
	got, err := VerificationURL(Production, Query{
		NIF:            "B72877814",
		InvoiceNumber:  "1",
		ExpeditionDate: "01-01-2025",
		TotalAmount:    "20",
	})
	if err != nil {
		t.Fatalf("VerificationURL() error = %v", err)
	}
	want := "https://www2.agenciatributaria.gob.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=1&fecha=01-01-2025&importe=20"
	if got != want {
		t.Fatalf("VerificationURL() = %s, want %s", got, want)
	}
}

func TestVerificationURLNormalizesAmount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // importe query value
	}{
		{"dot with trailing zero", "131.40", "131.4"},
		{"comma decimal", "131,4", "131.4"},
		{"comma with trailing zeros", "131,40", "131.4"},
		{"already minimal", "131.4", "131.4"},
		{"integer", "20", "20"},
		{"integer with zero decimals", "20.00", "20"},
		{"zero decimals one place", "0.4", "0.4"},
		{"one decimal zero", "0,40", "0.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerificationURL(Testing, Query{
				NIF:            "B72877814",
				InvoiceNumber:  "GITHUB-EJ-004",
				ExpeditionDate: "04-11-2024",
				TotalAmount:    tt.input,
			})
			if err != nil {
				t.Fatalf("VerificationURL(%q) error = %v", tt.input, err)
			}
			want := "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=" + tt.want
			if got != want {
				t.Fatalf("VerificationURL(%q) = %s, want %s", tt.input, got, want)
			}
		})
	}
}

func TestVerificationURLErrorsAreSpanish(t *testing.T) {
	tests := []struct {
		name    string
		env     Environment
		query   Query
		wantMsg string
	}{
		{
			name:    "unknown environment",
			env:     Environment(42),
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "04-11-2024", TotalAmount: "1"},
			wantMsg: "entorno de cotejo desconocido",
		},
		{
			name:    "missing nif",
			env:     Testing,
			query:   Query{InvoiceNumber: "1", ExpeditionDate: "04-11-2024", TotalAmount: "1"},
			wantMsg: "NIF del emisor es obligatorio",
		},
		{
			name:    "missing invoice number",
			env:     Testing,
			query:   Query{NIF: "B72877814", ExpeditionDate: "04-11-2024", TotalAmount: "1"},
			wantMsg: "número de series de la factura es obligatorio",
		},
		{
			name:    "bad date format",
			env:     Testing,
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "2024-11-04", TotalAmount: "1"},
			wantMsg: "fecha de expedición debe tener formato DD-MM-AAAA",
		},
		{
			name:    "impossible calendar date",
			env:     Testing,
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "32-01-2024", TotalAmount: "1"},
			wantMsg: "fecha de expedición debe tener formato DD-MM-AAAA",
		},
		{
			name:    "bad amount",
			env:     Testing,
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "04-11-2024", TotalAmount: "abc"},
			wantMsg: "importe debe ser un número decimal",
		},
		{
			name:    "negative amount",
			env:     Testing,
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "04-11-2024", TotalAmount: "-5"},
			wantMsg: "importe debe ser un número decimal",
		},
		{
			name:    "three decimals",
			env:     Testing,
			query:   Query{NIF: "B72877814", InvoiceNumber: "1", ExpeditionDate: "04-11-2024", TotalAmount: "1.234"},
			wantMsg: "importe debe ser un número decimal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerificationURL(tt.env, tt.query)
			if err == nil {
				t.Fatalf("VerificationURL() expected error containing %q, got nil", tt.wantMsg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("VerificationURL() error = %q, want it to contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestParseVerificationURL(t *testing.T) {
	tests := []struct {
		name       string
		rawurl     string
		wantEnv    Environment
		wantQuery  Query
		wantErrMsg string
	}{
		{
			name:      "spec testing URL round-trips",
			rawurl:    "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4",
			wantEnv:   Testing,
			wantQuery: Query{NIF: "B72877814", InvoiceNumber: "GITHUB-EJ-004", ExpeditionDate: "04-11-2024", TotalAmount: "131.4"},
		},
		{
			name:      "production URL",
			rawurl:    "https://www2.agenciatributaria.gob.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=A1&fecha=01-01-2025&importe=20.00",
			wantEnv:   Production,
			wantQuery: Query{NIF: "B72877814", InvoiceNumber: "A1", ExpeditionDate: "01-01-2025", TotalAmount: "20"},
		},
		{
			name:       "wrong scheme",
			rawurl:     "http://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01-01-2025&importe=1",
			wantErrMsg: "https",
		},
		{
			name:       "unknown host",
			rawurl:     "https://example.com/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01-01-2025&importe=1",
			wantErrMsg: "no corresponde a ningún entorno de cotejo",
		},
		{
			name:       "wrong path",
			rawurl:     "https://prewww2.aeat.es/wlpl/TIKE-CONT/Otro?nif=B&numserie=1&fecha=01-01-2025&importe=1",
			wantErrMsg: "ruta",
		},
		{
			name:       "missing parameter",
			rawurl:     "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01-01-2025",
			wantErrMsg: "importe",
		},
		{
			name:       "extra parameter (no fingerprint allowed in the QR)",
			rawurl:     "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01-01-2025&importe=1&huella=DEADBEEF",
			wantErrMsg: "parámetros adicionales",
		},
		{
			name:       "invalid date inside URL",
			rawurl:     "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01/01/2025&importe=1",
			wantErrMsg: "fecha de expedición debe tener formato DD-MM-AAAA",
		},
		{
			name:       "invalid amount inside URL",
			rawurl:     "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B&numserie=1&fecha=01-01-2025&importe=1,2,3",
			wantErrMsg: "importe debe ser un número decimal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, q, err := ParseVerificationURL(tt.rawurl)
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatalf("ParseVerificationURL() expected error containing %q, got %+v %+v", tt.wantErrMsg, env, q)
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Fatalf("ParseVerificationURL() error = %q, want it to contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVerificationURL() error = %v", err)
			}
			if env != tt.wantEnv {
				t.Fatalf("ParseVerificationURL() env = %v, want %v", env, tt.wantEnv)
			}
			if q != tt.wantQuery {
				t.Fatalf("ParseVerificationURL() query = %+v, want %+v", q, tt.wantQuery)
			}
		})
	}
}

func TestParseAndRebuildRoundTrip(t *testing.T) {
	raw := "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4"
	env, q, err := ParseVerificationURL(raw)
	if err != nil {
		t.Fatalf("ParseVerificationURL() error = %v", err)
	}
	rebuilt, err := VerificationURL(env, q)
	if err != nil {
		t.Fatalf("VerificationURL() error = %v", err)
	}
	if rebuilt != raw {
		t.Fatalf("round trip = %s, want %s", rebuilt, raw)
	}
}
