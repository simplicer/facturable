package qrcode

import "fmt"

// MaxInputBytes caps the QR payload (the cotejo URL) at 2000 bytes, the QR
// theoretical maximum (audit network/input #7). The encoder only supports
// versions 1..10 — capacity well below this cap — but the explicit limit
// documents the bound and rejects abuse before any matrix work.
const MaxInputBytes = 2000

// Matrix is the finished QR symbol: true = dark module.
type Matrix struct {
	Version int
	Size    int
	M       [][]bool
}

// Encode builds the complete QR symbol for data (byte mode, level M,
// versions 1..10) choosing the least-penalized mask.
func Encode(data []byte) (*Matrix, error) {
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("el contenido del QR (%d bytes) supera el máximo de %d bytes", len(data), MaxInputBytes)
	}
	version, cw, err := Codewords(data)
	if err != nil {
		return nil, err
	}
	size := version*4 + 17

	best := -1
	var bestM [][]bool
	for mask := 0; mask < 8; mask++ {
		m := buildMatrix(version, size, cw, mask)
		p := penalty(m)
		if best == -1 || p < best {
			best, bestM = p, m
		}
	}
	return &Matrix{Version: version, Size: size, M: bestM}, nil
}

func buildMatrix(version, size int, cw []byte, mask int) [][]bool {
	m := newMatrix(size)
	reservado := newMatrix(size) // funciones ya fijadas

	placeFunctions(m, reservado, version, size)
	placeData(m, reservado, cw, size)
	applyMask(m, reservado, mask, size)
	placeFormatInfo(m, mask, size)
	if version >= 7 {
		placeVersionInfo(m, version, size)
	}
	return m
}

func newMatrix(size int) [][]bool {
	m := make([][]bool, size)
	for i := range m {
		m[i] = make([]bool, size)
	}
	return m
}

// placeFunctions draws the finder patterns, separators, alignment,
// timing, dark module and the reserved format/version info areas.
func placeFunctions(m, reservado [][]bool, version, size int) {
	// finder patterns + separators
	ponerFinder := func(row, col int) {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				r, c := row+dr, col+dc
				if r < 0 || r >= size || c < 0 || c >= size {
					continue
				}
				oscuro := (dr >= 0 && dr <= 6 && (dc == 0 || dc == 6)) ||
					(dc >= 0 && dc <= 6 && (dr == 0 || dr == 6)) ||
					(dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4)
				m[r][c] = oscuro
				reservado[r][c] = true
			}
		}
	}
	ponerFinder(0, 0)
	ponerFinder(0, size-7)
	ponerFinder(size-7, 0)

	// timing (row 6 and column 6)
	for i := 8; i < size-8; i++ {
		oscuro := i%2 == 0
		m[6][i] = oscuro
		reservado[6][i] = true
		m[i][6] = oscuro
		reservado[i][6] = true
	}

	// alignment
	if centros, ok := alignmentCenters[version]; ok {
		for _, r := range centros {
			for _, c := range centros {
				// se superpone con finders: saltar
				if (r <= 8 && c <= 8) || (r <= 8 && c >= size-9) || (r >= size-9 && c <= 8) {
					continue
				}
				for dr := -2; dr <= 2; dr++ {
					for dc := -2; dc <= 2; dc++ {
						oscuro := dr == -2 || dr == 2 || dc == -2 || dc == 2 || (dr == 0 && dc == 0)
						m[r+dr][c+dc] = oscuro
						reservado[r+dr][c+dc] = true
					}
				}
			}
		}
	}

	// EXACT reservation of format info (15 bits in two copies + dark module):
	// column 8: rows 0..8 (bits 0..7 and timing) and rows size-7..size-1
	// (bits 8..14); the dark module sits at (size-8, 8).
	// row 8: columns 0..8 and columns size-8..size-1.
	for i := 0; i <= 8; i++ {
		reservado[8][i] = true
		reservado[i][8] = true
	}
	for i := 0; i <= 7; i++ {
		reservado[8][size-1-i] = true
		reservado[size-1-i][8] = true
	}
	m[size-8][8] = true
	reservado[size-8][8] = true

	// reserva version info (v>=7): bloques de 3 templateColumns x 6 filas junto a
	// los finders superior-derecho e inferior-izquierdo
	if version >= 7 {
		for i := 0; i < 6; i++ {
			for j := 0; j < 3; j++ {
				reservado[i][size-11+j] = true
				reservado[size-11+j][i] = true
			}
		}
	}
}

// placeData coloca los codewords en zigzag desde la esquina inferior
// derecha, saltando la columna 6 y las zonas reservadas.
func placeData(m, reservado [][]bool, cw []byte, size int) {
	bit := 0
	totalBits := len(cw) * 8
	down := true
	for col := size - 1; col > 0; col -= 2 {
		if col == 6 {
			col = 5 // skip the timing column
		}
		for i := 0; i < size; i++ {
			var row int
			if down {
				row = size - 1 - i
			} else {
				row = i
			}
			for _, c := range []int{col, col - 1} {
				if reservado[row][c] {
					continue
				}
				if bit < totalBits {
					m[row][c] = cw[bit/8]&(1<<uint(7-bit%8)) != 0
				}
				bit++
			}
		}
		down = !down
	}
}

// applyMask applies the given mask to the data modules.
func applyMask(m, reservado [][]bool, mask, size int) {
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if reservado[r][c] {
				continue
			}
			if maskBit(mask, r, c) {
				m[r][c] = !m[r][c]
			}
		}
	}
}

func maskBit(mask, r, c int) bool {
	switch mask {
	case 0:
		return (r+c)%2 == 0
	case 1:
		return r%2 == 0
	case 2:
		return c%3 == 0
	case 3:
		return (r+c)%3 == 0
	case 4:
		return (r/2+c/3)%2 == 0
	case 5:
		return (r*c)%2+(r*c)%3 == 0
	case 6:
		return ((r*c)%2+(r*c)%3)%2 == 0
	default:
		return ((r+c)%2+(r*c)%3)%2 == 0
	}
}

// placeFormatInfo writes the 15 bits (two copies) for level M + mask:
//
//	vertical (column 8): bits 0..5 in rows 0..5, bits 6..7 in rows 7..8,
//	  bits 8..14 in rows size-7..size-1 (LSB on top).
//	horizontal (row 8): bits 0..7 in columns size-1..size-8, bit 8 in
//	  column 7, bits 9..14 in columns 5..0 (LSB on the right).
//
// Plus the fixed dark module at (size-8, 8).
func placeFormatInfo(m [][]bool, mask, size int) {
	fi := formatInfo(mask)
	bit := func(i int) bool { return fi&(1<<uint(i)) != 0 }

	for i := 0; i < 6; i++ {
		m[i][8] = bit(i)
	}
	m[7][8] = bit(6)
	m[8][8] = bit(7)
	for i := 8; i < 15; i++ {
		m[size-15+i][8] = bit(i)
	}

	for i := 0; i < 8; i++ {
		m[8][size-1-i] = bit(i)
	}
	m[8][7] = bit(8)
	for i := 9; i < 15; i++ {
		m[8][14-i] = bit(i)
	}

	m[size-8][8] = true // dark module
}

// formatInfo computes the 15 bits for level M (00) and the given mask.
func formatInfo(mask int) int {
	data := 0b00000 | mask // M = 00 en los dos bits altos
	fi := data << 10
	for i := 14; i >= 10; i-- {
		if fi&(1<<uint(i)) != 0 {
			fi ^= formatInfoGen << uint(i-10)
		}
	}
	return (data<<10 | fi) ^ formatInfoMask
}

// placeVersionInfo escribe los 18 bits (v>=7) en sus dos bloques de 6x3:
// bloque superior-derecho row-major con el bit MENOS significativo en
// (0, size-11); el inferior-izquierdo es su traspuesto (ISO 18004).
func placeVersionInfo(m [][]bool, version, size int) {
	vi := versionInfo(version)
	for i := 0; i < 18; i++ {
		bit := vi&(1<<uint(i)) != 0
		m[i/3][i%3+size-11] = bit
		m[i%3+size-11][i/3] = bit
	}
}

func versionInfo(version int) int {
	vi := version << 12
	for i := 17; i >= 12; i-- {
		if vi&(1<<uint(i)) != 0 {
			vi ^= versionInfoGen << uint(i-12)
		}
	}
	return version<<12 | vi
}

// ---- ISO 18004 penalty (mask selection) ----

func penalty(m [][]bool) int {
	size := len(m)
	p := 0

	// rule 1: 5+ consecutive equal modules in rows/columns
	for i := 0; i < size; i++ {
		p += runs(m, i, true)
		p += runs(m, i, false)
	}

	// regla 2: bloques 2x2 del mismo color
	for r := 0; r < size-1; r++ {
		for c := 0; c < size-1; c++ {
			if m[r][c] == m[r][c+1] && m[r][c] == m[r+1][c] && m[r][c] == m[r+1][c+1] {
				p += 3
			}
		}
	}

	// regla 3: patrones tipo finder (1011101 con 4 claros a un lado)
	patron1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	patron2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	for r := 0; r < size; r++ {
		for c := 0; c+11 <= size; c++ {
			match1, match2 := true, true
			for k := 0; k < 11; k++ {
				if m[r][c+k] != patron1[k] {
					match1 = false
				}
				if m[r][c+k] != patron2[k] {
					match2 = false
				}
			}
			if match1 || match2 {
				p += 40
			}
		}
	}
	for c := 0; c < size; c++ {
		for r := 0; r+11 <= size; r++ {
			match1, match2 := true, true
			for k := 0; k < 11; k++ {
				if m[r+k][c] != patron1[k] {
					match1 = false
				}
				if m[r+k][c] != patron2[k] {
					match2 = false
				}
			}
			if match1 || match2 {
				p += 40
			}
		}
	}

	// rule 4: dark proportion
	oscuras := 0
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if m[r][c] {
				oscuras++
			}
		}
	}
	total := size * size
	// rule 4 with floating point arithmetic, like the reference
	// computation: int(abs(percentage*100 - 50) / 5) * 10
	porcentaje := float64(oscuras) * 100 / float64(total)
	p += int(absF(porcentaje-50)/5) * 10
	return p
}

func runs(m [][]bool, i int, row bool) int {
	size := len(m)
	p := 0
	run, prev := 0, -1
	for j := 0; j < size; j++ {
		var v int
		if row {
			v = b2i(m[i][j])
		} else {
			v = b2i(m[j][i])
		}
		if v == prev {
			run++
		} else {
			run = 1
			prev = v
		}
		if run == 5 {
			p += 3
		} else if run > 5 {
			p++
		}
	}
	return p
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
