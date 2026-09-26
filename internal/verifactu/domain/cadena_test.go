package domain

import (
	"testing"
)

func mustChainPieces(t *testing.T) (Enlace, Enlace, Enlace) {
	t.Helper()
	serie1 := mustSerie(t, "F-2027-0001")
	serie2 := mustSerie(t, "F-2027-0002")
	fecha := mustFecha(t, "05-01-2027")
	huella1 := mustHuella(t, "1111111111111111111111111111111111111111111111111111111111111111")
	huella2 := mustHuella(t, "2222222222222222222222222222222222222222222222222222222222222222")
	n2 := Enlace{Serie: serie1, Fecha: fecha, Huella: huella1}          // chainRow real n-2
	n1 := Enlace{Serie: serie2, Fecha: fecha, Huella: huella2}          // identidad de n-1
	return n1, n2, Enlace{Serie: serie1, Fecha: fecha, Huella: huella1} // n2Referenciado
}

func TestVerificarSalto(t *testing.T) {
	n1, n2, n2ref := mustChainPieces(t)

	t.Run("primer chainRow de la cadena", func(t *testing.T) {
		ultimo := RegistroVigente{Enlace: n1, Referencia: nil}
		if err := VerificarSalto(ultimo, nil); err != nil {
			t.Fatalf("VerificarSalto() = %v, want nil", err)
		}
	})

	t.Run("salto consistente n-2 n-1", func(t *testing.T) {
		ultimo := RegistroVigente{Enlace: n1, Referencia: &n2ref}
		if err := VerificarSalto(ultimo, &n2); err != nil {
			t.Fatalf("VerificarSalto() = %v, want nil", err)
		}
	})

	t.Run("huella del enlace manipulada", func(t *testing.T) {
		falsa := n2ref
		falsa.Huella = mustHuella(t, "3333333333333333333333333333333333333333333333333333333333333333")
		ultimo := RegistroVigente{Enlace: n1, Referencia: &falsa}
		err := VerificarSalto(ultimo, &n2)
		if err == nil {
			t.Fatal("VerificarSalto() = nil, want rupture error")
		}
	})

	t.Run("chainRow omitido: n-1 referencia a un n-2 desconocido", func(t *testing.T) {
		ultimo := RegistroVigente{Enlace: n1, Referencia: &n2ref}
		err := VerificarSalto(ultimo, nil)
		if err == nil {
			t.Fatal("VerificarSalto() = nil, want missing-predecessor error")
		}
	})

	t.Run("n-1 declara ser primero pero existe n-2", func(t *testing.T) {
		ultimo := RegistroVigente{Enlace: n1, Referencia: nil}
		err := VerificarSalto(ultimo, &n2)
		if err == nil {
			t.Fatal("VerificarSalto() = nil, want inconsistency error")
		}
	})
}
