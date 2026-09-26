// Package qrcode implementa un codificador QR propio (modo byte, nivel M,
// versiones 1-10) para estampar la URL de cotejo de la AEAT en las facturas.
// Sin dependencias externas: GF(256), Reed-Solomon, entrelazado, máscaras y
// penalización según ISO/IEC 18004, verificado contra generadores de
// referencia (véase qrcode_test.go).
package qrcode

import "fmt"

// ---- tablas ISO 18004 (nivel M, versiones 1..10) ----

type blockSpec struct {
	totalCodewords int
	ecPerBlock     int
	g1Blocks       int
	g1Data         int
	g2Blocks       int
	g2Data         int
}

// specByVersion[n] describe la versión n a nivel M.
var specByVersion = map[int]blockSpec{
	1:  {26, 10, 1, 16, 0, 0},
	2:  {44, 16, 1, 28, 0, 0},
	3:  {70, 26, 1, 44, 0, 0},
	4:  {100, 18, 2, 32, 0, 0},
	5:  {134, 24, 2, 43, 0, 0},
	6:  {172, 16, 4, 27, 0, 0},
	7:  {196, 18, 4, 31, 0, 0},
	8:  {242, 22, 2, 38, 2, 39},
	9:  {292, 22, 3, 36, 2, 37},
	10: {346, 26, 4, 43, 1, 44},
}

// alignmentCenters por versión (2..10); la versión 1 no lleva.
var alignmentCenters = map[int][]int{
	2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
	7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50},
}

const (
	formatInfoMask = 0x5412
	formatInfoGen  = 0x537
	versionInfoGen = 0x1F25
)

// ---- GF(256), polinomio primitivo 0x11D ----

var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// rsGenerator devuelve el polinomio generador de grado n (coeficientes,
// término de mayor grado primero).
func rsGenerator(n int) []byte {
	gen := []byte{1}
	for i := 0; i < n; i++ {
		next := make([]byte, len(gen)+1)
		for j, c := range gen {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		gen = next
	}
	return gen
}

// rsEncode calcula los n símbolos de corrección de un bloque de datos.
func rsEncode(data []byte, n int) []byte {
	gen := rsGenerator(n)
	rem := make([]byte, n)
	for _, d := range data {
		factor := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		if factor != 0 {
			for i := 0; i < n; i++ {
				rem[i] ^= gfMul(gen[i+1], factor)
			}
		}
	}
	return rem
}

// ---- construcción de codewords ----

// Codewords produce la secuencia completa (datos + EC entrelazados) para
// data en modo byte y nivel M, eligiendo la menor versión 1..10 que la
// contenga.
func Codewords(data []byte) (version int, cw []byte, err error) {
	spec, version, err := chooseVersion(len(data))
	if err != nil {
		return 0, nil, err
	}

	// flujo de bits: modo 0100 (byte) + contador (8 bits, v1..9 y v10..40
	// usa 16, pero con v<=10 el contador es 8 para v1..9; para v10 son 16)
	countBits := 8
	if version == 10 {
		countBits = 16
	}
	bits := newBitStream()
	bits.append(0b0100, 4)
	bits.append(len(data), countBits)
	for _, b := range data {
		bits.append(int(b), 8)
	}

	// relleno hasta frontera de byte, luego pad 0xEC/0x11
	dataCodewords := bits.bytes()
	totalData := spec.totalData()
	for len(dataCodewords) < totalData {
		dataCodewords = append(dataCodewords, 0xEC, 0x11)
	}
	dataCodewords = dataCodewords[:totalData]

	// bloques + EC
	var blocks [][]byte
	pos := 0
	for i := 0; i < spec.g1Blocks; i++ {
		blocks = append(blocks, dataCodewords[pos:pos+spec.g1Data])
		pos += spec.g1Data
	}
	for i := 0; i < spec.g2Blocks; i++ {
		blocks = append(blocks, dataCodewords[pos:pos+spec.g2Data])
		pos += spec.g2Data
	}
	ecBlocks := make([][]byte, len(blocks))
	for i, b := range blocks {
		ecBlocks[i] = rsEncode(b, spec.ecPerBlock)
	}

	// entrelazado
	var out []byte
	maxData := spec.g1Data
	if spec.g2Data > maxData {
		maxData = spec.g2Data
	}
	for i := 0; i < maxData; i++ {
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for _, b := range ecBlocks {
			out = append(out, b[i])
		}
	}
	return version, out, nil
}

func (s blockSpec) totalData() int { return s.g1Blocks*s.g1Data + s.g2Blocks*s.g2Data }

func chooseVersion(n int) (blockSpec, int, error) {
	for v := 1; v <= 10; v++ {
		spec := specByVersion[v]
		countBits := 8
		if v == 10 {
			countBits = 16
		}
		if 4+countBits+n*8 <= spec.totalData()*8 {
			return spec, v, nil
		}
	}
	return blockSpec{}, 0, fmt.Errorf("el contenido (%d bytes) no cabe en QR versión 10 nivel M; acorta la URL", n)
}

// ---- flujo de bits ----

type bitStream struct {
	bits []byte
	n    int
}

func newBitStream() *bitStream { return &bitStream{} }

func (s *bitStream) append(value, width int) {
	for i := width - 1; i >= 0; i-- {
		s.n++
		s.ensure()
		if value&(1<<i) != 0 {
			s.bits[(s.n-1)/8] |= 1 << (7 - uint((s.n-1)%8))
		}
	}
}

func (s *bitStream) ensure() {
	for len(s.bits)*8 < s.n {
		s.bits = append(s.bits, 0)
	}
}

func (s *bitStream) bytes() []byte { return s.bits }
