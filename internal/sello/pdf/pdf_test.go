package pdf

import (
	"bytes"
	"regexp"
	"strconv"
	"testing"
)

func ejemplo() DatosFactura {
	return DatosFactura{
		EmpresaNombre: "Facturable Test SL", EmpresaNIF: "B72877814",
		Serie: "F-2027-0001", Date: "05-01-2027", Tipo: "F1",
		ClienteNombre: "ACME SL (ñ á é)", ClienteNIF: "B44531218",
		Description: "Servicios de enero",
		Desgloses:   []DesgloseFactura{{Tipo: "21", Base: "1000.00", Cuota: "210.00"}},
		CuotaTotal:  "210.00", ImporteTotal: "1210.00",
		EstadoAEAT: "correcto", CSV: "ABC123DEF456GH78",
		URLCotejo: "https://www2.agenciatributaria.gob.es/wlpl/TIKE-CONT/ValidarQR?nif=B72877814&numserie=F-2027-0001&fecha=05-01-2027&importe=1210.00",
	}
}

func TestFacturaPDFEstructura(t *testing.T) {
	b, err := Factura(ejemplo())
	if err != nil {
		t.Fatalf("Factura(): %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("sin header PDF")
	}
	if !bytes.Contains(b, []byte("%%EOF")) || !bytes.Contains(b, []byte("/Type /Page")) ||
		!bytes.Contains(b, []byte("/BaseFont /Helvetica")) {
		t.Fatal("estructura PDF incompleta")
	}
	// xref: los offsets deben apuntar a "N 0 obj"
	re := regexp.MustCompile(`(?m)^(\d{10}) 00000 n`)
	ms := re.FindAllSubmatch(b, -1)
	if len(ms) != 5 {
		t.Fatalf("xref con %d entradas, want 5", len(ms))
	}
	for _, m := range ms {
		off := int(m[1][0]-'0')*1_000_000_000 + int(m[1][1]-'0')*100_000_000 +
			int(m[1][2]-'0')*10_000_000 + int(m[1][3]-'0')*1_000_000 +
			int(m[1][4]-'0')*100_000 + int(m[1][5]-'0')*10_000 +
			int(m[1][6]-'0')*1_000 + int(m[1][7]-'0')*100 +
			int(m[1][8]-'0')*10 + int(m[1][9]-'0')
		rest := b[off:]
		if !bytes.HasPrefix(rest, []byte("1 0 obj")) &&
			!regexp.MustCompile(`^\d 0 obj`).Match(rest) {
			t.Fatalf("offset %d no apunta a un objeto", off)
		}
	}
}

func TestFacturaPDFContenido(t *testing.T) {
	b, err := Factura(ejemplo())
	if err != nil {
		t.Fatal(err)
	}
	for _, trozo := range []string{
		"FACTURA F-2027-0001", "VERI*FACTU", "ABC123DEF456GH78",
		"TOTAL: 1210.00 EUR", "re f", // QR dibujado con rects
	} {
		if !bytes.Contains(b, []byte(trozo)) {
			t.Fatalf("el PDF no contiene %q", trozo)
		}
	}
}

func TestFacturaPDFSinURL(t *testing.T) {
	d := ejemplo()
	d.URLCotejo = ""
	b, err := Factura(d)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(" re f")) {
		t.Fatal("sin URL no debería dibujar QR")
	}
}

// TestQRGeometriaLegal: el QR impreso mide 30mm de lado (mínimo legal),
// centrado horizontalmente y junto al margen superior (FAQ AEAT).
func TestQRGeometriaLegal(t *testing.T) {
	b, err := Factura(ejemplo())
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`([\d.]+) ([\d.]+) ([\d.]+) ([\d.]+) re f`)
	minX, minY, maxX, maxY := 1e9, 1e9, -1e9, -1e9
	rects := 0
	for _, m := range re.FindAllSubmatch(b, -1) {
		x, y, w := atof(string(m[1])), atof(string(m[2])), atof(string(m[3]))
		h := atof(string(m[4]))
		// los módulos del QR son cuadrados: descarta cualquier otro rect
		if w == 0 || w != h {
			continue
		}
		rects++
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
		if x+w > maxX {
			maxX = x + w
		}
		if y+w > maxY {
			maxY = y + w
		}
	}
	if rects == 0 {
		t.Fatal("el PDF no dibuja ningún QR")
	}
	sideMM := (maxX - minX) * 25.4 / 72
	if sideMM < 29.9 || sideMM > 30.1 {
		t.Fatalf("lado del QR = %.2fmm, want 30.00mm", sideMM)
	}
	if (maxY-minY)*25.4/72 < 29.9 {
		t.Fatalf("alto del QR = %.2fmm", (maxY-minY)*25.4/72)
	}
	if minX < 235 || minX > 265 {
		t.Fatalf("QR no centrado: minX=%.1f (queríamos ~252)", minX)
	}
	if maxY < 800 || maxY > 815 {
		t.Fatalf("QR no está arriba: maxY=%.1f (queríamos ~812)", maxY)
	}
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
