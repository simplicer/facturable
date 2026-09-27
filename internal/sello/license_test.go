package sello

import (
	"context"
	"strings"
	"testing"
)

// TestSubmitPendingLicenseGate: when the agent wires a LicenseCheck into the
// Service, submissions only go out with a valid license; no gate keeps the
// unlicensed development mode open.
func TestSubmitPendingLicenseGate(t *testing.T) {
	cases := []struct {
		name          string
		gate          func() (bool, string)
		wantErrSubstr string
		wantSent      int
		wantEnvelopes int
	}{
		{
			name:          "no gate configured stays open",
			gate:          nil,
			wantErrSubstr: "",
			wantSent:      2,
			wantEnvelopes: 1,
		},
		{
			name:          "valid license submits",
			gate:          func() (bool, string) { return true, "" },
			wantErrSubstr: "",
			wantSent:      2,
			wantEnvelopes: 1,
		},
		{
			name:          "rejected license blocks submission",
			gate:          func() (bool, string) { return false, "la licencia no autoriza este NIF" },
			wantErrSubstr: "licencia",
			wantSent:      0,
			wantEnvelopes: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, remisor, _, _ := newTestService(t)
			srv.LicenseCheck = tc.gate
			if _, err := srv.IngestCSV(strings.NewReader(csvPrueba)); err != nil {
				t.Fatalf("IngestCSV(): %v", err)
			}
			n, err := srv.SubmitPending(context.Background())
			if tc.wantErrSubstr == "" {
				if err != nil {
					t.Fatalf("SubmitPending(): %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("SubmitPending() sent %d, want error containing %q", n, tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErrSubstr)
				}
			}
			if n != tc.wantSent {
				t.Fatalf("sent = %d, want %d", n, tc.wantSent)
			}
			if len(remisor.recibidos) != tc.wantEnvelopes {
				t.Fatalf("envelopes = %d, want %d", len(remisor.recibidos), tc.wantEnvelopes)
			}
		})
	}
}
