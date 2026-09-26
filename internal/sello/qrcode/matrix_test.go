package qrcode

import (
	"os"
	"strings"
	"testing"
)

const urlAEAT = "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4"

// TestMatrizGolden compara el símbolo COMPLETO con la matriz de la librería
// de referencia Python `qrcode` (misma URL, nivel M, borde 0): valida
// colocación, máscaras, elección de máscara por penalización, format info
// y version info módulo a módulo.
func TestMatrizGolden(t *testing.T) {
	ref, err := os.ReadFile("testdata/qr_aeat_url_v7_m.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Encode([]byte(urlAEAT))
	if err != nil {
		t.Fatal(err)
	}
	lineas := strings.Split(strings.TrimSpace(string(ref)), "\n")
	if len(lineas) != got.Size {
		t.Fatalf("size = %d, referencia = %d", got.Size, len(lineas))
	}
	diff := 0
	for r := 0; r < got.Size; r++ {
		for c := 0; c < got.Size; c++ {
			want := lineas[r][c] == '1'
			if got.M[r][c] != want {
				diff++
			}
		}
	}
	if diff > 0 {
		// primeras diferencias para diagnóstico
		var ejemplos []string
		for r := 0; r < got.Size && len(ejemplos) < 8; r++ {
			for c := 0; c < got.Size && len(ejemplos) < 8; c++ {
				want := lineas[r][c] == '1'
				if got.M[r][c] != want {
					ejemplos = append(ejemplos, sprintPos(r, c, got.M[r][c], want))
				}
			}
		}
		t.Fatalf("%d módulos difieren de la referencia; ejemplos: %s",
			diff, strings.Join(ejemplos, ", "))
	}
}

func sprintPos(r, c int, got, want bool) string {
	return "(f" + itoa(r) + ",c" + itoa(c) + " got=" + itoa(b2i(got)) + " want=" + itoa(b2i(want)) + ")"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

func TestEstructuraBasica(t *testing.T) {
	m, err := Encode([]byte(urlAEAT))
	if err != nil {
		t.Fatal(err)
	}
	size := m.Size

	// finders 7x7 con borde blanco: esquinas (0,0), (0,size-7), (size-7,0)
	finders := [][2]int{{0, 0}, {0, size - 7}, {size - 7, 0}}
	for _, f := range finders {
		r0, c0 := f[0], f[1]
		for dr := 0; dr < 7; dr++ {
			for dc := 0; dc < 7; dc++ {
				want := dr == 0 || dr == 6 || dc == 0 || dc == 6 ||
					(dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)
				if m.M[r0+dr][c0+dc] != want {
					t.Fatalf("finder (%d,%d) módulo (%d,%d) mal", r0, c0, dr, dc)
				}
			}
		}
	}

	// temporización fila 6
	for i := 8; i < size-8; i++ {
		if m.M[6][i] != (i%2 == 0) {
			t.Fatalf("temporización fila 6 col %d", i)
		}
	}

	// módulo oscuro
	if !m.M[size-8][8] {
		t.Fatal("módulo oscuro apagado")
	}

	// format info: las dos copias deben ser iguales
	for i := 0; i <= 5; i++ {
		if m.M[8][i] != m.M[size-1-i][8] && i <= 6 {
			t.Fatalf("copias de format info difieren (bit %d)", i)
		}
	}
}

// TestFormatInfoBCH: el formato debe pasar el check BCH tras quitar la máscara fija.
func TestFormatInfoBCH(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		fi := formatInfo(mask)
		raw := fi ^ formatInfoMask
		data := raw >> 10
		rem := raw
		for i := 14; i >= 10; i-- {
			if rem&(1<<uint(i)) != 0 {
				rem ^= formatInfoGen << uint(i-10)
			}
		}
		if rem&0x3FF != 0 {
			t.Fatalf("máscara %d: format info BCH inválido", mask)
		}
		if (data>>3)&0b11 != 0b00 { // nivel M en los bits altos
			t.Fatalf("máscara %d: nivel distinto de M (%02b)", mask, (data>>3)&0b11)
		}
		if int(data&0b111) != mask {
			t.Fatalf("máscara %d: bits de máscara incorrectos (%d)", mask, data&0b111)
		}
	}
}

// TestVersionInfoBCH valida los 18 bits para v>=7.
func TestVersionInfoBCH(t *testing.T) {
	for v := 7; v <= 10; v++ {
		vi := versionInfo(v)
		rem := vi
		for i := 17; i >= 12; i-- {
			if rem&(1<<uint(i)) != 0 {
				rem ^= versionInfoGen << uint(i-12)
			}
		}
		if rem&0xFFF != 0 {
			t.Fatalf("versión %d: BCH inválido", v)
		}
		if vi>>12 != v {
			t.Fatalf("versión %d: bits de versión incorrectos", v)
		}
	}
}
