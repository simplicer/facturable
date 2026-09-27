// Package pdf writes the stamped-invoice PDF (A4, base Helvetica, QR drawn
// as rectangles). Minimal in-house PDF 1.4 writer: no dependencies,
// auditable (ADR-003).
package pdf

import (
	"bytes"
	"fmt"
	"strings"

	"facturable/internal/sello/qrcode"
)

// QR geometry per the official FAQ (possibility of remission section): the QR must
// measure between 30x30 and 40x40 mm (TicketBAI range), sit at the START of
// the invoice before any content, top-center for portrait layouts, and
// appear exactly once on the first page.
const (
	// QRSizeMM is the printed QR side: the legal MINIMUM (FAQ AEAT: between
	// 30x30 and 40x40 mm; user decision: always the minimum). Independent
	// of the symbol version: the module size adapts.
	QRSizeMM = 30.0
	// PointsPerMM for A4 PDF geometry (72pt / 25.4mm).
	PointsPerMM = 72.0 / 25.4
	// pageWidth of A4 in points.
	pageWidth = 595
)

// DatosFactura es todo lo que se imprime en el PDF sellado.
type DatosFactura struct {
	EmpresaNombre, EmpresaNIF string
	Serie, Date, Tipo         string
	ClienteNombre, ClienteNIF string
	Description               string
	Desgloses                 []DesgloseFactura
	CuotaTotal, ImporteTotal  string
	EstadoAEAT, CSV           string
	URLCotejo                 string // URL del QR (generada con domain/qr)
}

// DesgloseFactura is one printed VAT line.
type DesgloseFactura struct {
	Tipo, Base, Cuota string
}

// Factura construye el PDF de una factura.
func Factura(d DatosFactura) ([]byte, error) {
	c := &contenido{b: &bytes.Buffer{}}

	// header
	c.texto(40, 800, 16, d.EmpresaNombre)
	c.texto(40, 784, 10, "NIF: "+d.EmpresaNIF)
	c.texto(400, 800, 16, "FACTURA "+d.Serie)
	c.texto(400, 784, 10, "Date: "+d.Date+"   Tipo: "+d.Tipo)

	// client and description
	c.texto(40, 740, 11, "CLIENTO")
	c.texto(40, 726, 10, d.ClienteNombre)
	c.texto(40, 713, 10, "NIF: "+d.ClienteNIF)
	c.linea(40, 700, 555, 700)

	// desgloses
	c.texto(40, 684, 10, "IVA%    Base imponible    Cuota")
	y := 668
	for _, g := range d.Desgloses {
		c.texto(40, y, 10, fmt.Sprintf("%s%%    %s EUR    %s EUR", g.Tipo, g.Base, g.Cuota))
		y -= 14
	}
	c.texto(40, y-6, 11, fmt.Sprintf("Cuota total: %s EUR", d.CuotaTotal))
	c.texto(40, y-20, 12, fmt.Sprintf("TOTAL: %s EUR", d.ImporteTotal))

	// QR: primer elemento, arriba centrado junto al margen superior (FAQ:
	// 30-40mm; user decision: minimum 30mm), before any content.
	if d.URLCotejo != "" {
		m, err := qrcode.Encode([]byte(d.URLCotejo))
		if err != nil {
			return nil, fmt.Errorf("no se pudo generar el QR: %v", err)
		}
		qrSidePt := QRSizeMM * PointsPerMM
		escala := qrSidePt / float64(m.Size)
		dibujarQR(c, m, int((pageWidth-qrSidePt)/2), 812, escala)
	}

	// pie normativo
	c.texto(40, 60, 10, "VERI*FACTU — Factura verificable en la sede electrónica de la AEAT")
	c.texto(40, 47, 9, "Registro: "+d.EstadoAEAT+"   CSV: "+d.CSV)
	c.texto(40, 34, 7, d.URLCotejo)

	return c.pdf(), nil
}

// drawQR paints the matrix with the computed module size (30mm side).
func dibujarQR(c *contenido, m *qrcode.Matrix, x, yBase int, escala float64) {
	for r := 0; r < m.Size; r++ {
		for col := 0; col < m.Size; col++ {
			if m.M[r][col] {
				x0 := float64(x) + float64(col)*escala
				y0 := float64(yBase) - float64(r+1)*escala
				fmt.Fprintf(c.b, "%s %s %s %s re f\n",
					fnum(x0), fnum(y0), fnum(escala), fnum(escala))
			}
		}
	}
}

// ---- infraestructura PDF ----

type contenido struct{ b *bytes.Buffer }

func (c *contenido) texto(x, y, size int, s string) {
	fmt.Fprintf(c.b, "BT /F1 %d Tf %s %s Td (%s) Tj ET\n", size, fnum(float64(x)), fnum(float64(y)), escapar(latin1(s)))
}

func (c *contenido) linea(x0, y0, x1, y1 int) {
	fmt.Fprintf(c.b, "%s %s m %s %s l S\n", fnum(float64(x0)), fnum(float64(y0)), fnum(float64(x1)), fnum(float64(y1)))
}

// assemblePDF builds the file: catalog, pages, page, font and content.
func (c *contenido) pdf() []byte {
	var objs []string
	stream := c.b.String()
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	)

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return out.Bytes()
}

func fnum(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func escapar(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	return strings.ReplaceAll(s, ")", "\\)")
}

// latin1 convierte a Windows-1252 lo imprimible con Helvetica WinAnsi.
func latin1(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteRune(r)
		case r >= 0xA0 && r <= 0xFF:
			b.WriteByte(byte(r))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
