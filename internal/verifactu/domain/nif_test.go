package domain

import "testing"

func TestNewNIFValid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// DNIs (control letter verified against TRWAGMYFPDXBNJZSQVHLCKE mod 23).
		{name: "dni with valid letter", raw: "12345678Z", want: "12345678Z"},
		{name: "dni lowercase normalizes", raw: "12345678z", want: "12345678Z"},
		{name: "dni padded is accepted", raw: "  12345678Z ", want: "12345678Z"},
		{name: "dni from official huella example", raw: "89890001K", want: "89890001K"},
		{name: "dni starting with zero", raw: "00000000T", want: "00000000T"},
		// NIEs: X -> 0, Y -> 1, Z -> 2 before the DNI control check.
		{name: "nie foreigner X", raw: "X1234567L", want: "X1234567L"},
		{name: "nie foreigner Y", raw: "Y1234567X", want: "Y1234567X"},
		{name: "nie lowercase normalizes", raw: "x1234567l", want: "X1234567L"},
		// CIFs (control computed with the even/odd digit-sum algorithm).
		{name: "cif sociedad limitiva digit control", raw: "B72877814", want: "B72877814"},
		{name: "cif buyer of aeat examples", raw: "B44531218", want: "B44531218"},
		{name: "cif sociedad anonima digit control", raw: "A46103834", want: "A46103834"},
		{name: "cif organo publico letter control", raw: "P4620000B", want: "P4620000B"},
		{name: "cif old personal format K with letter control", raw: "K1234567D", want: "K1234567D"},
		{name: "cif old personal format K with digit control", raw: "K12345674", want: "K12345674"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewNIF(tt.raw)
			if err != nil {
				t.Fatalf("NewNIF(%q) unexpected error: %v", tt.raw, err)
			}
			if got.Value() != tt.want {
				t.Fatalf("NewNIF(%q).Value() = %q, want %q", tt.raw, got.Value(), tt.want)
			}
		})
	}
}

func TestNewNIFInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "only spaces", raw: "   "},
		{name: "dni wrong control letter", raw: "12345678A"},
		{name: "dni too short", raw: "1234567Z"},
		{name: "dni too long", raw: "123456789Z"},
		{name: "dni letters in number", raw: "1234567AZ"},
		{name: "nie unknown first letter", raw: "A1234567L"},
		{name: "nie wrong control letter", raw: "X1234567A"},
		{name: "nie too short", raw: "X123456L"},
		{name: "cif wrong digit control", raw: "B72877815"},
		{name: "cif digit-required org with letter control", raw: "A4610383D"},
		{name: "cif letter-required org with digit control", raw: "P46200002"},
		{name: "cif flexible org with wrong control of either form", raw: "C1234567Z"},
		{name: "cif unknown org letter", raw: "I23456789"},
		{name: "cif missing control", raw: "B7287781"},
		{name: "random text", raw: "not-a-nif"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewNIF(tt.raw); err == nil {
				t.Fatalf("NewNIF(%q) expected error, got none", tt.raw)
			}
		})
	}
}

func TestNewNIFErrorIsInSpanish(t *testing.T) {
	_, err := NewNIF("12345678A")
	if err == nil {
		t.Fatal("expected error for invalid NIF")
	}
	// Human-facing errors follow the repo rule: Spanish messages.
	assertSpanish(t, err.Error())
}

func assertSpanish(t *testing.T, msg string) {
	t.Helper()
	// Minimal guard: must not be an English diagnostic like "invalid NIF".
	for _, banned := range []string{"invalid", "must be", "expected"} {
		if containsWord(msg, banned) {
			t.Fatalf("error message %q looks like English diagnostics (found %q)", msg, banned)
		}
	}
}

func containsWord(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
