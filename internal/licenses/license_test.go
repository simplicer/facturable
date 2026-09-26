package licenses

import (
	"strings"
	"testing"
	"time"
)

func TestNewLicenseDefaults(t *testing.T) {
	t.Parallel()

	issued := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)

	t.Run("zero grace days defaults to seven", func(t *testing.T) {
		l := NewLicense("lic-1", PlanPro, []string{"B12345678"}, issued, expires, 0)
		if l.GraceDays != DefaultGraceDays {
			t.Fatalf("GraceDays = %d, want %d", l.GraceDays, DefaultGraceDays)
		}
	})

	t.Run("explicit grace days is honored", func(t *testing.T) {
		l := NewLicense("lic-1", PlanPro, nil, issued, expires, 30)
		if l.GraceDays != 30 {
			t.Fatalf("GraceDays = %d, want 30", l.GraceDays)
		}
	})

	t.Run("fields are kept verbatim", func(t *testing.T) {
		nifs := []string{"B12345678", "12345678Z"}
		l := NewLicense("lic-1", PlanGestoria, nifs, issued, expires, 0)
		if l.LicenseID != "lic-1" || l.Plan != PlanGestoria {
			t.Fatalf("license = %+v, want id lic-1 plan gestoria", l)
		}
		if len(l.NIFs) != 2 || l.NIFs[0] != "B12345678" || l.NIFs[1] != "12345678Z" {
			t.Fatalf("NIFs = %v, want [B12345678 12345678Z]", l.NIFs)
		}
		if !l.IssuedAt.Equal(issued) || !l.ExpiresAt.Equal(expires) {
			t.Fatalf("dates = %v/%v, want %v/%v", l.IssuedAt, l.ExpiresAt, issued, expires)
		}
	})
}

func TestLicenseValidate(t *testing.T) {
	t.Parallel()

	valid := NewLicense("lic-1", PlanBasico, nil,
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), 0)

	tests := []struct {
		name    string
		mutate  func(l *License)
		wantErr string // empty means valid
	}{
		{name: "valid license", mutate: func(l *License) {}, wantErr: ""},
		{
			name:    "missing identifier",
			mutate:  func(l *License) { l.LicenseID = "" },
			wantErr: "identificador",
		},
		{
			name:    "unknown plan",
			mutate:  func(l *License) { l.Plan = Plan("ultra") },
			wantErr: "plan",
		},
		{
			name:    "zero expiry",
			mutate:  func(l *License) { l.ExpiresAt = time.Time{} },
			wantErr: "caducidad",
		},
		{
			name:    "zero issue date",
			mutate:  func(l *License) { l.IssuedAt = time.Time{} },
			wantErr: "emisión",
		},
		{
			name: "expiry not after issue",
			mutate: func(l *License) {
				l.ExpiresAt = l.IssuedAt
			},
			wantErr: "caducidad",
		},
		{
			name:    "negative grace days",
			mutate:  func(l *License) { l.GraceDays = -1 },
			wantErr: "gracia",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := valid
			l.NIFs = append([]string(nil), valid.NIFs...)
			tt.mutate(&l)

			err := l.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %q, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLicenseAuthorizesNIF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		nifs []string
		nif  string
		want bool
	}{
		{name: "empty list is unrestricted", nifs: nil, nif: "B12345678", want: true},
		{name: "listed NIF is authorized", nifs: []string{"B12345678", "12345678Z"}, nif: "12345678Z", want: true},
		{name: "unlisted NIF is rejected", nifs: []string{"B12345678"}, nif: "B87654321", want: false},
		{name: "case and spaces are ignored", nifs: []string{"b12345678"}, nif: "  B12345678 ", want: true},
		{name: "empty NIF on restricted license is rejected", nifs: []string{"B12345678"}, nif: "", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := NewLicense("lic-1", PlanBasico, tt.nifs,
				time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), 0)
			if got := l.AuthorizesNIF(tt.nif); got != tt.want {
				t.Fatalf("AuthorizesNIF(%q) = %v, want %v", tt.nif, got, tt.want)
			}
		})
	}
}
