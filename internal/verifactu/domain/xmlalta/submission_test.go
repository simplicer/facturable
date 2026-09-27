package xmlalta

import (
	"fmt"
	"strings"
	"testing"
)

// repeatBlock clones one valid block n times: the batch-size boundary tests
// need a full 1000/1001-block batch without inflating the fixture.
func repeatBlock(b string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// validBlock returns a genuine <sum:RegistroFactura> block produced by this
// same package, so the corrupt-block tests mutate something realistic.
func validBlock(t *testing.T) string {
	t.Helper()
	submission := NewSubmission(Issuer{Name: "EMPISA SL", TaxID: "B72877814"}, validRegistration())
	soapXML, err := submission.XML()
	if err != nil {
		t.Fatalf("XML(): %v", err)
	}
	blocks := SplitRecordBlocks(soapXML)
	if len(blocks) != 1 {
		t.Fatalf("SplitRecordBlocks() = %d blocks, want 1", len(blocks))
	}
	return blocks[0]
}

func TestBatchFromBlocksValidatesBlocks(t *testing.T) {
	good := validBlock(t)
	cases := []struct {
		name       string
		blocks     []string
		wantErr    string // substring of the expected error; empty means success
		inEnvelope string // substring the assembled envelope must contain on success
	}{
		{
			name:       "valid block assembles",
			blocks:     []string{good},
			wantErr:    "",
			inEnvelope: "<sum1:NumSerieFactura>12345</sum1:NumSerieFactura>",
		},
		{
			name:    "block missing IDVersion is rejected",
			blocks:  []string{strings.Replace(good, "<sum1:IDVersion>1.0</sum1:IDVersion>", "", 1)},
			wantErr: "bloque de registro 1 corrupto",
		},
		{
			name:    "block missing Huella is rejected",
			blocks:  []string{strings.ReplaceAll(good, "<sum1:Huella>", "")},
			wantErr: "bloque de registro 1 corrupto",
		},
		{
			name:    "corrupt block reports its position",
			blocks:  []string{good, "truncated garbage"},
			wantErr: "bloque de registro 2 corrupto",
		},
		{
			name:    "empty batch is rejected",
			blocks:  nil,
			wantErr: "no hay registros para remitir",
		},
		{
			name:    "batch above the AEAT maximum is rejected",
			blocks:  repeatBlock(good, MaxRecordsPerBatch+1),
			wantErr: fmt.Sprintf("máximo %d registros por lote (especificación AEAT)", MaxRecordsPerBatch),
		},
		{
			name:       "batch of exactly the AEAT maximum assembles",
			blocks:     repeatBlock(good, MaxRecordsPerBatch),
			wantErr:    "",
			inEnvelope: "<sum:RegFactuSistemaFacturacion>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope, err := BatchFromBlocks("EMPISA SL", "B72877814", tc.blocks)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("BatchFromBlocks(): %v", err)
				}
				if !strings.Contains(envelope, tc.inEnvelope) {
					t.Fatalf("envelope missing record payload:\n%s", envelope)
				}
				return
			}
			if err == nil {
				t.Fatalf("BatchFromBlocks() = envelope, want error %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
