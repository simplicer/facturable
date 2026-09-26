package xmlalta

import (
	"strings"
	"testing"
)

func validRegistration() RegistrationRecord {
	record := NewRegistrationRecord()
	record.InvoiceID = InvoiceID{IssuerID: "B72877814", Number: "12345", ExpeditionDate: "13-09-2024"}
	record.IssuerName = "EMPISA SL"
	record.InvoiceType = "F1"
	record.OperationDescription = "Prestación de servicios"
	record.Recipients = []Recipient{{Name: "CLIENTE SL", TaxID: "B44531218"}}
	record.Breakdown = []BreakdownLine{{
		RegimeKey:              "01",
		OperationQualification: "S1",
		TaxRate:                "21",
		TaxBase:                "100",
		TaxFee:                 "21",
	}}
	record.TotalTax = "21"
	record.TotalAmount = "121"
	record.SimplifiedInvoice = ""
	record.Chaining = &Chaining{PreviousRecord: &PreviousRecord{
		IssuerID:       "B72877814",
		Number:         "12344",
		ExpeditionDate: "12-09-2024",
		Fingerprint:    "4EECCE4DD48C0539665385D61D451BA921B7160CA6FEF46CD3C2E2BC5C778E14",
	}}
	record.System = SoftwareSystem{
		Name:                "Facturable S.L.",
		TaxID:               "B00000000",
		SystemName:          "Facturable Sello",
		SystemID:            "F1",
		Version:             "1.0",
		InstallationNumber:  "1",
		VerifactuOnlyUse:    "S",
		MultiTaxpayerUse:    "N",
		MultipleOTIndicator: "N",
	}
	record.GeneratedAt = "2024-09-13T19:20:30+01:00"
	record.Fingerprint = "8C8DCEFB120522E0C71BC19902F44D5334FF6C98E74F0E3AC1D1E5A30C2EA836"
	return record
}

func validCancellation() CancellationRecord {
	record := NewCancellationRecord()
	record.InvoiceID = CancelledInvoiceID{IssuerID: "B72877814", Number: "12345", ExpeditionDate: "13-09-2024"}
	record.Chaining = &Chaining{PreviousRecord: &PreviousRecord{
		Fingerprint: "4EECCE4DD48C0539665385D61D451BA921B7160CA6FEF46CD3C2E2BC5C778E14",
	}}
	record.System = validRegistration().System
	record.GeneratedAt = "2024-09-13T19:20:30+01:00"
	record.Fingerprint = "8C8DCEFB120522E0C71BC19902F44D5334FF6C98E74F0E3AC1D1E5A30C2EA836"
	return record
}

func TestValidRecordsPass(t *testing.T) {
	submission := NewSubmission(Issuer{Name: "EMPISA SL", TaxID: "B72877814"},
		validRegistration(), validCancellation())
	if err := submission.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestRegistrationRecordValidation(t *testing.T) {
	const validFingerprint = "4EECCE4DD48C0539665385D61D451BA921B7160CA6FEF46CD3C2E2BC5C778E14"

	tests := []struct {
		name     string
		mutate   func(*RegistrationRecord)
		wantMsgs []string
	}{
		{
			name:     "wrong schema version",
			mutate:   func(r *RegistrationRecord) { r.Version = "2.0" },
			wantMsgs: []string{`la versión del registro debe ser 1.0: "2.0"`},
		},
		{
			name: "missing issuer id",
			mutate: func(r *RegistrationRecord) {
				r.InvoiceID.IssuerID = ""
			},
			wantMsgs: []string{"el identificador del emisor de la factura (IDEmisorFactura) es obligatorio"},
		},
		{
			name: "bad expedition date",
			mutate: func(r *RegistrationRecord) {
				r.InvoiceID.ExpeditionDate = "2024-09-13"
			},
			wantMsgs: []string{"la fecha de expedición de la factura debe tener formato DD-MM-AAAA"},
		},
		{
			name: "impossible calendar date",
			mutate: func(r *RegistrationRecord) {
				r.InvoiceID.ExpeditionDate = "31-02-2024"
			},
			wantMsgs: []string{"la fecha de expedición de la factura debe tener formato DD-MM-AAAA"},
		},
		{
			name:     "missing invoice type",
			mutate:   func(r *RegistrationRecord) { r.InvoiceType = "" },
			wantMsgs: []string{"el tipo de factura (TipoFactura) es obligatorio"},
		},
		{
			name:     "empty breakdown",
			mutate:   func(r *RegistrationRecord) { r.Breakdown = nil },
			wantMsgs: []string{"el desglose (Desglose) debe incluir al menos un detalle"},
		},
		{
			name: "breakdown above the twelve-line limit",
			mutate: func(r *RegistrationRecord) {
				r.Breakdown = make([]BreakdownLine, MaxBreakdownLines+1)
				for i := range r.Breakdown {
					r.Breakdown[i] = BreakdownLine{RegimeKey: "01", OperationQualification: "S1", TaxBase: "10", TaxFee: "0"}
				}
			},
			wantMsgs: []string{"el desglose (Desglose) no puede contener más de 12 detalles: tiene 13"},
		},
		{
			name: "twelve breakdown lines are allowed",
			mutate: func(r *RegistrationRecord) {
				r.Breakdown = make([]BreakdownLine, MaxBreakdownLines)
				for i := range r.Breakdown {
					r.Breakdown[i] = BreakdownLine{RegimeKey: "01", OperationQualification: "S1", TaxBase: "10", TaxFee: "0"}
				}
			},
			wantMsgs: nil,
		},
		{
			name: "breakdown line without regime key",
			mutate: func(r *RegistrationRecord) {
				r.Breakdown[0].RegimeKey = ""
			},
			wantMsgs: []string{"el detalle 1 del desglose debe indicar la clave del régimen (ClaveRegimen)"},
		},
		{
			name: "breakdown line with malformed amount",
			mutate: func(r *RegistrationRecord) {
				r.Breakdown[0].TaxBase = "10,5"
			},
			wantMsgs: []string{"base imponible (BaseImponibleOimporteNoSujeto) debe ser un importe decimal válido"},
		},
		{
			name:     "missing total amount",
			mutate:   func(r *RegistrationRecord) { r.TotalAmount = "" },
			wantMsgs: []string{"el importe total (ImporteTotal) es obligatorio"},
		},
		{
			name: "system id longer than two characters",
			mutate: func(r *RegistrationRecord) {
				r.System.SystemID = "F1A"
			},
			wantMsgs: []string{"el identificador del sistema informático (IdSistemaInformatico) debe tener exactamente 2 caracteres"},
		},
		{
			name: "system flag invalid",
			mutate: func(r *RegistrationRecord) {
				r.System.MultipleOTIndicator = "X"
			},
			wantMsgs: []string{`el indicador IndicadorMultiplesOT solo admite S o N: "X"`},
		},
		{
			name: "generated at without timezone",
			mutate: func(r *RegistrationRecord) {
				r.GeneratedAt = "2024-09-13T19:20:30"
			},
			wantMsgs: []string{"la fecha y hora de generación del registro (FechaHoraHusoGenRegistro) debe incluir el huso horario"},
		},
		{
			name: "wrong fingerprint type",
			mutate: func(r *RegistrationRecord) {
				r.FingerprintType = "02"
			},
			wantMsgs: []string{`el tipo de huella (TipoHuella) debe ser 01: "02"`},
		},
		{
			name: "lowercase fingerprint",
			mutate: func(r *RegistrationRecord) {
				r.Fingerprint = strings.ToLower(validFingerprint)
			},
			wantMsgs: []string{"la huella (Huella) debe ser el SHA-256 en hexadecimal mayúsculas de 64 caracteres"},
		},
		{
			name: "short previous fingerprint",
			mutate: func(r *RegistrationRecord) {
				r.Chaining.PreviousRecord.Fingerprint = "DEADBEEF"
			},
			wantMsgs: []string{"la huella del registro anterior (Huella) debe ser el SHA-256 en hexadecimal mayúsculas de 64 caracteres"},
		},
		{
			name: "first record marker without previous record",
			mutate: func(r *RegistrationRecord) {
				r.Chaining = &Chaining{FirstRecord: "S"}
			},
			wantMsgs: nil,
		},
		{
			name: "invalid first record marker",
			mutate: func(r *RegistrationRecord) {
				r.Chaining = &Chaining{FirstRecord: "X"}
			},
			wantMsgs: []string{`el indicador de primer registro (PrimerRegistro) solo admite S: "X"`},
		},
		{
			name: "missing chaining",
			mutate: func(r *RegistrationRecord) {
				r.Chaining = nil
			},
			wantMsgs: []string{"el encadenamiento (Encadenamiento) es obligatorio"},
		},
		{
			name: "several violations are reported together",
			mutate: func(r *RegistrationRecord) {
				r.InvoiceType = ""
				r.TotalTax = "not-an-amount"
			},
			wantMsgs: []string{
				"el tipo de factura (TipoFactura) es obligatorio",
				"la cuota total (CuotaTotal) debe ser un importe decimal válido",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validRegistration()
			tt.mutate(&record)
			err := record.Validate()
			if tt.wantMsgs == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected errors %v, got nil", tt.wantMsgs)
			}
			for _, want := range tt.wantMsgs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Validate() error\n%q\ndoes not contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestCancellationRecordValidation(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*CancellationRecord)
		wantMsgs []string
	}{
		{
			name:     "missing cancelled invoice number",
			mutate:   func(r *CancellationRecord) { r.InvoiceID.Number = "" },
			wantMsgs: []string{"el número de serie de la factura anulada (NumSerieFacturaAnulada) es obligatorio"},
		},
		{
			name: "bad cancelled expedition date",
			mutate: func(r *CancellationRecord) {
				r.InvoiceID.ExpeditionDate = "13/09/2024"
			},
			wantMsgs: []string{"la fecha de expedición de la factura anulada debe tener formato DD-MM-AAAA"},
		},
		{
			name: "malformed fingerprint",
			mutate: func(r *CancellationRecord) {
				r.Fingerprint = "no-es-una-huella"
			},
			wantMsgs: []string{"la huella (Huella) debe ser el SHA-256 en hexadecimal mayúsculas de 64 caracteres"},
		},
		{
			name: "first record chaining is allowed",
			mutate: func(r *CancellationRecord) {
				r.Chaining = &Chaining{FirstRecord: "S"}
			},
			wantMsgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validCancellation()
			tt.mutate(&record)
			err := record.Validate()
			if tt.wantMsgs == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected errors %v, got nil", tt.wantMsgs)
			}
			for _, want := range tt.wantMsgs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Validate() error\n%q\ndoes not contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestSubmissionValidation(t *testing.T) {
	t.Run("empty submission", func(t *testing.T) {
		err := NewSubmission(Issuer{Name: "EMPISA SL", TaxID: "B72877814"}).Validate()
		if err == nil || !strings.Contains(err.Error(), "la remisión debe incluir al menos un registro de facturación") {
			t.Fatalf("Validate() = %v, want empty-records error", err)
		}
	})

	t.Run("missing issuer", func(t *testing.T) {
		submission := NewSubmission(Issuer{}, validRegistration())
		err := submission.Validate()
		if err == nil {
			t.Fatal("Validate() expected issuer errors, got nil")
		}
		for _, want := range []string{"obligado de emisión es obligatorio", "NIF del obligado de emisión es obligatorio"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Validate() error %q does not contain %q", err.Error(), want)
			}
		}
	})

	t.Run("record violations carry the record position", func(t *testing.T) {
		bad := validCancellation()
		bad.Fingerprint = "no-es-una-huella"
		err := NewSubmission(Issuer{Name: "E", TaxID: "X"}, validRegistration(), bad).Validate()
		if err == nil || !strings.Contains(err.Error(), "registro 2: la huella (Huella) debe ser el SHA-256") {
			t.Fatalf("Validate() = %v, want position-prefixed error", err)
		}
	})
}

func TestOptionalElementsAreOmitted(t *testing.T) {
	got, err := NewSubmission(Issuer{Name: "XXXXX", TaxID: "AAAA"}, validRegistration()).XML()
	if err != nil {
		t.Fatalf("XML() error = %v", err)
	}
	for _, forbidden := range []string{"Subsanacion", "RechazoPrevio", "FacturaSimplificada", "PrimerRegistro", "Impuesto"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("XML() contains optional element %s that must be omitted:\n%s", forbidden, got)
		}
	}
}

func TestMixedBatchKeepsRecordOrder(t *testing.T) {
	submission := NewSubmission(Issuer{Name: "XXXXX", TaxID: "AAAA"}, validRegistration(), validCancellation())
	got, err := submission.XML()
	if err != nil {
		t.Fatalf("XML() error = %v", err)
	}
	if gotCount := strings.Count(got, "<sum:RegistroFactura>"); gotCount != 2 {
		t.Fatalf("XML() has %d RegistroFactura wrappers, want 2", gotCount)
	}
	altaAt := strings.Index(got, "<sum1:RegistroAlta>")
	anulacionAt := strings.Index(got, "<sum1:RegistroAnulacion>")
	if altaAt < 0 || anulacionAt < 0 || altaAt > anulacionAt {
		t.Fatalf("XML() lost the record order (alta=%d, anulacion=%d)", altaAt, anulacionAt)
	}
}

func TestNamespaceConstantsMatchSpec(t *testing.T) {
	// Values verified in /tmp/vf/spec_sweb.txt lines 1867-1871 (the PDF text
	// wraps the URLs; they are joined here as the schema names them).
	tests := []struct{ name, got, want string }{
		{"soapenv", NamespaceSOAPEnvelope, "http://schemas.xmlsoap.org/soap/envelope/"},
		{"sum", NamespaceSuministroLR, "https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroLR.xsd"},
		{"sum1", NamespaceSuministroInfo, "https://www2.agenciatributaria.gob.es/static_files/common/internet/dep/aplicaciones/es/aeat/tike/cont/ws/SuministroInformacion.xsd"},
		{"xd", NamespaceXMLDSig, "http://www.w3.org/2000/09/xmldsig#"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("namespace %s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestFixedSchemaValues(t *testing.T) {
	if IDVersion != "1.0" {
		t.Errorf("IDVersion = %q, want 1.0", IDVersion)
	}
	if FingerprintTypeSHA256 != "01" {
		t.Errorf("FingerprintTypeSHA256 = %q, want 01", FingerprintTypeSHA256)
	}
	record := NewRegistrationRecord()
	if record.Version != IDVersion || record.FingerprintType != FingerprintTypeSHA256 {
		t.Errorf("NewRegistrationRecord() does not set the fixed fields: %+v", record)
	}
	cancellation := NewCancellationRecord()
	if cancellation.Version != IDVersion || cancellation.FingerprintType != FingerprintTypeSHA256 {
		t.Errorf("NewCancellationRecord() does not set the fixed fields: %+v", cancellation)
	}
}
