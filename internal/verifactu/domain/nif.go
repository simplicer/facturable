package domain

import (
	"fmt"
	"strings"
)

// controlLetterTable maps a number modulo 23 to the DNI/NIE control letter
// (Real Decreto 1553/2004, article 4).
const controlLetterTable = "TRWAGMYFPDXBNJZSQVHLCKE"

// cifControlLetters maps a computed control digit (0-9) to its letter form,
// required for some organisation types (Orden HAC/1177/2024 annex, NIF
// design; Hacienda CIF control rules).
const cifControlLetters = "JABCDEFGHI"

// NIF is the Spanish tax identifier of a party: a DNI (8 digits + control
// letter), a NIE (X/Y/Z + 7 digits + control letter) or a CIF (organisation
// letter + 7 digits + control digit/letter).
type NIF struct {
	value string
}

// NewNIF builds a validated NIF, normalizing case and surrounding spaces.
func NewNIF(raw string) (NIF, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return NIF{}, fmt.Errorf("el NIF no puede estar vacío")
	}
	switch {
	case isNIEFormat(value):
		if err := validateNIE(value); err != nil {
			return NIF{}, err
		}
	case isDNIFormat(value):
		if err := validateDNI(value); err != nil {
			return NIF{}, err
		}
	case isCIFFormat(value):
		if err := validateCIF(value); err != nil {
			return NIF{}, err
		}
	default:
		return NIF{}, fmt.Errorf(
			"el NIF %q no tiene un formato válido (se admite DNI de 8 dígitos más letra, NIE X/Y/Z más 7 dígitos y letra, o CIF letra más 7 dígitos y control)", raw)
	}
	return NIF{value: value}, nil
}

// Value returns the normalized identifier (uppercase, without spaces).
func (n NIF) Value() string {
	return n.value
}

// IsZero reports whether the identifier was never built.
func (n NIF) IsZero() bool {
	return n.value == ""
}

func (n NIF) String() string {
	return n.value
}

func isDNIFormat(value string) bool {
	if len(value) != 9 {
		return false
	}
	for _, r := range value[:8] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value[8] >= 'A' && value[8] <= 'Z'
}

func isNIEFormat(value string) bool {
	if len(value) != 9 {
		return false
	}
	if value[0] != 'X' && value[0] != 'Y' && value[0] != 'Z' {
		return false
	}
	return isDNIFormat("0" + value[1:9])
}

func isCIFFormat(value string) bool {
	if len(value) != 9 {
		return false
	}
	if !strings.ContainsRune("ABCDEFGHJKLMNPQRSUVW", rune(value[0])) {
		return false
	}
	for _, r := range value[1:8] {
		if r < '0' || r > '9' {
			return false
		}
	}
	last := value[8]
	return (last >= '0' && last <= '9') || (last >= 'A' && last <= 'Z')
}

func validateDNI(value string) error {
	want := controlLetterTable[numberModulo(value[:8])]
	if value[8] != want {
		return fmt.Errorf("el DNI %q no es válido: la letra de control debería ser %c", value, want)
	}
	return nil
}

func validateNIE(value string) error {
	// The NIE prefix contributes 0, 1 or 2 to the numeric part before the
	// DNI control check.
	prefix := strings.IndexRune("XYZ", rune(value[0]))
	dniEquivalent := fmt.Sprintf("%d%s", prefix, value[1:8])
	want := controlLetterTable[numberModulo(dniEquivalent)]
	if value[8] != want {
		return fmt.Errorf("el NIE %q no es válido: la letra de control debería ser %c", value, want)
	}
	return nil
}

func validateCIF(value string) error {
	digits := value[1:8]
	given := value[8]

	sumEven := int(digits[1]-'0') + int(digits[3]-'0') + int(digits[5]-'0')
	sumOdd := 0
	for _, position := range []int{0, 2, 4, 6} {
		doubled := 2 * int(digits[position]-'0')
		for doubled > 0 {
			sumOdd += doubled % 10
			doubled /= 10
		}
	}
	control := (10 - (sumEven+sumOdd)%10) % 10

	wantDigit := byte('0' + control)
	wantLetter := cifControlLetters[control]

	switch value[0] {
	case 'A', 'B', 'E', 'H': // sociedades: the control must be a digit.
		if given != wantDigit {
			return fmt.Errorf("el CIF %q no es válido: el dígito de control debería ser %c", value, wantDigit)
		}
	case 'N', 'P', 'Q', 'R', 'S', 'W': // public entities and non-profits: the control must be a letter.
		if given != wantLetter {
			return fmt.Errorf("el CIF %q no es válido: el carácter de control debería ser %c", value, wantLetter)
		}
	default: // remaining organisation types (K/L/M legacy included) accept either form.
		if given != wantDigit && given != wantLetter {
			return fmt.Errorf("el CIF %q no es válido: el control debería ser %c o %c", value, wantDigit, wantLetter)
		}
	}
	return nil
}

func numberModulo(digits string) int {
	number := 0
	for _, r := range digits {
		number = number*10 + int(r-'0')
	}
	return number % 23
}
