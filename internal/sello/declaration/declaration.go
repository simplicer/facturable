// Package declaration serves the producer's responsible declaration
// (declaración responsable, RRSIF art. 13-15): required to be visible in
// the system for every version, and delivered at acquisition time.
package declaration

import "fmt"

// Info identifies this exact SIF build (art. 15.1 OM HAC/1177/2024).
type Info struct {
	ProducerName    string // NombreRazon of the producer
	ProducerNIF     string
	ProducerContact string // address + email
	SystemName      string // NombreSistemaInformatico
	SystemID        string // IdSistemaInformatico (2 chars)
	Version         string // full version of this build
	Installation    string // NumeroInstalacion
	OnlyVerifactu   bool   // TipoUsoPosibleSoloVerifactu = S
	MultiOT         bool   // TipoUsoPosibleMultiOT / IndicadorMultiplesOT
}

// Text renders the visible in-product declaration (Spanish: it is an
// end-user facing legal document).
func Text(i Info) string {
	sv, mo := "N", "N"
	if i.OnlyVerifactu {
		sv = "S"
	}
	if i.MultiOT {
		mo = "S"
	}
	return fmt.Sprintf(`DECLARACIÓN RESPONSABLE (art. 13-15 RRSIF, RD 1007/2023; art. 15 OM HAC/1177/2024)

El productor declara que el siguiente sistema informático de facturación (SIF) cumple el artículo 29.2.j) de la Ley 58/2003 (LGT), el RRSIF y las especificaciones de la OM HAC/1177/2024:

  Productor:              %s (NIF %s)
  Contacto del productor: %s

  Nombre del sistema:     %s
  Id. de sistema:         %s
  Versión:                %s
  Número de instalación:  %s
  TipoUsoPosibleSoloVerifactu: %s
  TipoUsoPosibleMultiOT:       %s
  IndicadorMultiplesOT:        %s

Modalidad de funcionamiento: VERI*FACTU (remisión inmediata de registros de facturación a la AEAT).
Los registros son inmutables (encadenados por huella SHA-256); toda corrección genera registros nuevos.
Esta declaración debe conservarse visible en el sistema durante toda su vida útil y entregarse al adquirirla.
`, i.ProducerName, i.ProducerNIF, i.ProducerContact, i.SystemName, i.SystemID,
		i.Version, i.Installation, sv, mo, mo)
}
