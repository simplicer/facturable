package domain

import "testing"

func TestClasificarErrorCatalogoCompleto(t *testing.T) {
	t.Parallel()
	if got := len(erroresAEAT); got != 52 {
		t.Fatalf("catálogo con %d códigos, se esperaban exactamente 52 (validaciones AEAT)", got)
	}
}

func TestClasificarError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		codigo       string
		categoria    CategoriaError
		reintentable bool
		aceptado     bool
	}{
		{"4102", CategoriaRechazoTotal, false, false},
		{"1106", CategoriaRechazoRegistro, false, false},
		{"1111", CategoriaRechazoRegistro, false, false},
		{"2001", CategoriaAviso, false, true},
		{"3000", CategoriaDuplicado, false, true},
		{"3500", CategoriaErrorAEAT, true, false},
		{"9999", CategoriaDesconocido, false, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.codigo, func(t *testing.T) {
			t.Parallel()
			info := ClasificarError(tt.codigo)
			if info.Categoria != tt.categoria || info.Reintentable != tt.reintentable || info.Aceptado != tt.aceptado {
				t.Fatalf("ClasificarError(%q) = %+v, want categoria=%s reintentable=%v aceptado=%v",
					tt.codigo, info, tt.categoria, tt.reintentable, tt.aceptado)
			}
			if info.Accion == "" || info.Description == "" {
				t.Fatalf("ClasificarError(%q) sin descripción o acción: %+v", tt.codigo, info)
			}
		})
	}
}
