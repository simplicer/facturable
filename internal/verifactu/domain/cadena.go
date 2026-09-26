package domain

import "fmt"

// Enlace identifies a previous record by the triple the AEAT chain embeds in
// every new record (Encadenamiento/RegistroAnterior): series, expedition
// date and fingerprint of the predecessor.
type Enlace struct {
	Serie  SerieNumero
	Fecha  FechaExpedicion
	Huella Huella
}

// IsZero reports whether the enlace points to nothing.
func (e Enlace) IsZero() bool {
	return e.Serie.IsZero() && e.Fecha.IsZero() && e.Huella.IsZero()
}

// RegistroVigente is the store view of the LAST record (n-1) needed to chain
// the next one: its own identity plus the enlace it declared towards its
// predecessor (nil when n-1 was the first record of the chain).
type RegistroVigente struct {
	Enlace             // identity of n-1 itself (series, date, own huella).
	Referencia *Enlace // what n-1 declared about n-2; nil if n-1 was first.
}

// VerificarSalto checks the chain link between n-2 and n-1 exactly as the
// AEAT FAQ requires before generating record n: the enlace that n-1 declares
// towards n-2 must match the real identity and huella of n-2 held by the
// store. This is what makes silent omissions and insertions detectable.
func VerificarSalto(ultimo RegistroVigente, n2 *Enlace) error {
	if ultimo.Referencia == nil {
		if n2 != nil {
			return fmt.Errorf(
				"la cadena está rota: el último chainRow %s declara ser el primero pero el almacén conoce un chainRow anterior (%s)",
				ultimo.Serie.Value(), n2.Serie.Value())
		}
		return nil
	}
	if n2 == nil {
		return fmt.Errorf(
			"la cadena está rota: el último chainRow %s referencia a %s pero el almacén no conserva ese chainRow anterior",
			ultimo.Serie.Value(), ultimo.Referencia.Serie.Value())
	}
	if *ultimo.Referencia != *n2 {
		return fmt.Errorf(
			"la cadena está rota: el último chainRow %s referencia a %s con huella %s, pero el chainRow real conservado es %s con huella %s",
			ultimo.Serie.Value(), ultimo.Referencia.Serie.Value(), ultimo.Referencia.Huella.Value(),
			n2.Serie.Value(), n2.Huella.Value())
	}
	return nil
}
