package xmlalta

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Formats of the AEAT schema, verified against the official message examples.
var (
	datePattern        = regexp.MustCompile(`^\d{2}-\d{2}-\d{4}$`)
	amountPattern      = regexp.MustCompile(`^-?\d+(\.\d{1,2})?$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9A-F]{64}$`)
	// DateTime with an explicit UTC offset (huso horario): the schema field
	// FechaHoraHusoGenRegistro requires the offset, never "Z".
	dateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?[+-]\d{2}:\d{2}$`)
)

// validDate reports whether value is a real calendar date in DD-MM-AAAA.
func validDate(value string) bool {
	if !datePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("02-01-2006", value)
	return err == nil
}

// validDateTime reports whether value is an ISO 8601 timestamp with explicit
// offset, e.g. 2024-09-13T19:20:30+01:00.
func validDateTime(value string) bool {
	if !dateTimePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

// validAmount reports whether value is a decimal amount with at most two
// decimals ("10", "0.4", "21.4"); negatives are allowed for rectificative
// records.
func validAmount(value string) bool {
	return amountPattern.MatchString(value)
}

// validateVersion enforces the fixed schema version.
func validateVersion(version string) []error {
	if version != IDVersion {
		return []error{fmt.Errorf("la versión del registro debe ser %s: %q", IDVersion, version)}
	}
	return nil
}

// validateAmountField appends a violation when the named amount is missing or
// malformed. fieldLabel is the human-readable Spanish description.
func validateAmountField(value, fieldLabel string) []error {
	if value == "" {
		return []error{fmt.Errorf("%s es obligatorio", fieldLabel)}
	}
	if !validAmount(value) {
		return []error{fmt.Errorf("%s debe ser un importe decimal válido: %q", fieldLabel, value)}
	}
	return nil
}

// validateRecordFooter checks the parts shared by altas and anulaciones:
// chaining, producing software system, generation timestamp and fingerprint.
func validateRecordFooter(chaining *Chaining, system SoftwareSystem, generatedAt, fingerprintType, fingerprint string) []error {
	var errs []error
	errs = append(errs, validateChaining(chaining)...)
	errs = append(errs, validateSoftwareSystem(system)...)
	if !validDateTime(generatedAt) {
		errs = append(errs, fmt.Errorf("la fecha y hora de generación del registro (FechaHoraHusoGenRegistro) debe incluir el huso horario, p. ej. 2024-09-13T19:20:30+01:00: %q", generatedAt))
	}
	if fingerprintType != FingerprintTypeSHA256 {
		errs = append(errs, fmt.Errorf("el tipo de huella (TipoHuella) debe ser %s: %q", FingerprintTypeSHA256, fingerprintType))
	}
	if !fingerprintPattern.MatchString(fingerprint) {
		errs = append(errs, fmt.Errorf("la huella (Huella) debe ser el SHA-256 en hexadecimal mayúsculas de 64 caracteres: %q", fingerprint))
	}
	return errs
}

func validateChaining(chaining *Chaining) []error {
	if chaining == nil {
		return []error{errors.New("el encadenamiento (Encadenamiento) es obligatorio")}
	}
	var errs []error
	switch chaining.FirstRecord {
	case "":
		// The record chains to a previous record instead.
	case "S":
		// First record of the chain.
	default:
		errs = append(errs, fmt.Errorf("el indicador de primer registro (PrimerRegistro) solo admite S: %q", chaining.FirstRecord))
	}
	if previous := chaining.PreviousRecord; previous != nil {
		if !fingerprintPattern.MatchString(previous.Fingerprint) {
			errs = append(errs, fmt.Errorf("la huella del registro anterior (Huella) debe ser el SHA-256 en hexadecimal mayúsculas de 64 caracteres: %q", previous.Fingerprint))
		}
		if previous.ExpeditionDate != "" && !validDate(previous.ExpeditionDate) {
			errs = append(errs, fmt.Errorf("la fecha de expedición del registro anterior debe tener formato DD-MM-AAAA: %q", previous.ExpeditionDate))
		}
	}
	return errs
}

func validateSoftwareSystem(system SoftwareSystem) []error {
	var errs []error
	if system.Name == "" {
		errs = append(errs, errors.New("el nombre o razón social del productor del sistema informático es obligatorio"))
	}
	if system.TaxID == "" {
		errs = append(errs, errors.New("el NIF del productor del sistema informático es obligatorio"))
	}
	if system.SystemName == "" {
		errs = append(errs, errors.New("el nombre del sistema informático (NombreSistemaInformatico) es obligatorio"))
	}
	if len(system.SystemID) != 2 {
		errs = append(errs, fmt.Errorf("el identificador del sistema informático (IdSistemaInformatico) debe tener exactamente 2 caracteres: %q", system.SystemID))
	}
	if system.Version == "" {
		errs = append(errs, errors.New("la versión del sistema informático (Version) es obligatoria"))
	}
	if system.InstallationNumber == "" {
		errs = append(errs, errors.New("el número de instalación (NumeroInstalacion) es obligatorio"))
	}
	errs = append(errs, validateSNFlag(system.VerifactuOnlyUse, "TipoUsoPosibleSoloVerifactu")...)
	errs = append(errs, validateSNFlag(system.MultiTaxpayerUse, "TipoUsoPosibleMultiOT")...)
	errs = append(errs, validateSNFlag(system.MultipleOTIndicator, "IndicadorMultiplesOT")...)
	return errs
}

func validateSNFlag(value, fieldLabel string) []error {
	if value != "S" && value != "N" {
		return []error{fmt.Errorf("el indicador %s solo admite S o N: %q", fieldLabel, value)}
	}
	return nil
}
