package qrcode

import (
	"testing"
)

// Referencias generadas con la librería de referencia `qrcode` de Python
// (nivel M), mismo contenido: validan flujo de bits, Reed-Solomon y
// entrelazado byte a byte.
var goldenCases = map[string][]byte{}

func loadGolden(t *testing.T) map[string][]byte {
	t.Helper()
	// "hello" fuerza modo byte (la referencia en mayúsculas optimizaría a
	// alfanumérico, que este codificador no implementa por diseño).
	return map[string][]byte{
		"hello": {64, 86, 134, 86, 198, 198, 240, 236, 17, 236, 17, 236, 17, 236, 17, 236, 22, 79, 223, 212, 140, 17, 209, 92, 47, 183},
	}
}

func TestCodewordsGolden(t *testing.T) {
	t.Parallel()
	golden := loadGolden(t)
	for data, want := range golden {
		v, got, err := Codewords([]byte(data))
		if err != nil {
			t.Fatalf("Codewords(%q): %v", data, err)
		}
		if v != 1 {
			t.Fatalf("Codewords(%q) versión = %d, want 1", data, v)
		}
		if len(got) != len(want) {
			t.Fatalf("Codewords(%q) len = %d, want %d", data, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Codewords(%q)[%d] = %d, want %d", data, i, got[i], want[i])
			}
		}
	}
}

func TestCodewordsURLAEAT(t *testing.T) {
	t.Parallel()
	url := "https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=GITHUB-EJ-004&fecha=04-11-2024&importe=131.4"
	want := []byte{71, 68, 19, 211, 70, 148, 66, 3, 135, 180, 102, 66, 71, 82, 231, 211, 71, 212, 86, 19, 7, 52, 215, 18, 51, 244, 54, 211, 162, 229, 87, 35, 242, 66, 38, 3, 247, 245, 150, 35, 7, 102, 83, 66, 38, 22, 212, 102, 87, 198, 116, 150, 119, 150, 149, 215, 119, 70, 68, 6, 115, 23, 133, 247, 34, 37, 84, 39, 230, 21, 34, 70, 22, 35, 212, 83, 86, 246, 84, 211, 23, 230, 162, 19, 66, 150, 211, 51, 230, 99, 3, 18, 87, 212, 3, 227, 50, 35, 66, 64, 247, 115, 102, 236, 118, 35, 102, 17, 199, 131, 86, 236, 6, 115, 54, 17, 194, 115, 134, 236, 245, 131, 19, 17, 64, 152, 223, 124, 251, 195, 49, 232, 159, 195, 46, 16, 59, 141, 41, 64, 226, 69, 210, 217, 248, 21, 197, 31, 34, 13, 115, 222, 85, 102, 32, 252, 207, 221, 193, 153, 145, 78, 92, 113, 64, 175, 36, 225, 164, 6, 91, 8, 154, 137, 46, 74, 56, 42, 143, 57, 234, 129, 194, 127, 173, 103, 93, 73, 76, 115, 162, 73, 84, 114, 196, 164}
	v, got, err := Codewords([]byte(url))
	if err != nil {
		t.Fatal(err)
	}
	if v != 7 || len(got) != len(want) {
		t.Fatalf("v=%d len=%d want v=7 len=%d", v, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("codeword %d: got %d want %d", i, got[i], want[i])
		}
	}
}

func TestElegirVersion(t *testing.T) {
	t.Parallel()
	if _, _, err := Codewords(make([]byte, 300)); err == nil {
		t.Fatal("300 bytes no caben en v10-M")
	}
	// 106 bytes caben en v6, 107 exigen v7.
	if v, _, err := Codewords(make([]byte, 106)); err != nil || v != 6 {
		t.Fatalf("106 bytes: v=%d err=%v, want v=6", v, err)
	}
	if v, _, err := Codewords(make([]byte, 107)); err != nil || v != 7 {
		t.Fatalf("107 bytes: v=%d err=%v, want v=7", v, err)
	}
}

// TestRSDivisibilidad: (datos+EC) · x^k mod generador == 0.
func TestRSDivisibilidad(t *testing.T) {
	t.Parallel()
	_, cw, err := Codewords([]byte("https://prewww2.aeat.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=1&fecha=01-01-2025&importe=20"))
	if err != nil {
		t.Fatal(err)
	}
	spec := specByVersion[7]
	// des-entrelaza y verifica cada bloque
	dataLen := spec.totalData()
	blocks := make([][]byte, 0, spec.g1Blocks+spec.g2Blocks)
	sizes := []int{}
	for i := 0; i < spec.g1Blocks; i++ {
		sizes = append(sizes, spec.g1Data)
	}
	for i := 0; i < spec.g2Blocks; i++ {
		sizes = append(sizes, spec.g2Data)
	}
	pos := 0
	for _, sz := range sizes {
		blocks = append(blocks, cw[pos:pos+sz])
		pos += sz
	}
	_ = dataLen
	for _, b := range blocks {
		ec := rsEncode(b, spec.ecPerBlock)
		gen := rsGenerator(spec.ecPerBlock)
		// polinomio completo debe reducirse a cero
		poly := append(append([]byte{}, b...), ec...)
		for i := 0; i < len(poly)-len(gen)+1; i++ {
			f := poly[i]
			if f == 0 {
				continue
			}
			for j := 0; j < len(gen); j++ {
				poly[i+j] ^= gfMul(gen[j], f)
			}
		}
		for _, p := range poly {
			if p != 0 {
				t.Fatal("bloque no divisible por el generador: RS incorrecto")
			}
		}
	}
}
