// Package pdf: extract.go pulls the text content from a PDF so fiscal
// fields can be parsed without OCR. Works on text-based PDFs (which most
// invoicing programs generate); image-only PDFs need the OCR fallback.
package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ExtractTextFromPDF pulls all displayable text from the PDF content
// streams. It decompresses FlateDecode streams and extracts the operands
// of text-showing operators (Tj, TJ, ', ").
func ExtractTextFromPDF(pdfBytes []byte) (string, error) {
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		return "", fmt.Errorf("no es un PDF válido")
	}
	pdf := string(pdfBytes)
	var allText strings.Builder

	// find all stream...endstream blocks
	streamRe := regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`)
	for _, match := range streamRe.FindAllStringSubmatch(pdf, -1) {
		raw := match[1]

		// try raw first (uncompressed)
		text := extractTextOperators(raw)
		if text != "" {
			allText.WriteString(text)
			allText.WriteString("\n")
			continue
		}

		// try zlib decompression (FlateDecode)
		if decompressed, err := zlibInflate([]byte(raw)); err == nil {
			text = extractTextOperators(string(decompressed))
			if text != "" {
				allText.WriteString(text)
				allText.WriteString("\n")
			}
		}
	}

	result := allText.String()
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("el PDF no contiene texto extraíble (probablemente es una imagen escaneada)")
	}
	return result, nil
}

// extractTextOperators finds text inside Tj, TJ, ' and " operators.
func extractTextOperators(content string) string {
	var out strings.Builder

	// (text) Tj
	reTj := regexp.MustCompile(`\(([^)]*)\)\s*Tj`)
	for _, m := range reTj.FindAllStringSubmatch(content, -1) {
		out.WriteString(unescapePDFString(m[1]))
		out.WriteString(" ")
	}

	// [(text1) (text2)] TJ
	reTJ := regexp.MustCompile(`\[([^\]]*)\]\s*TJ`)
	for _, m := range reTJ.FindAllStringSubmatch(content, -1) {
		for _, part := range strings.Split(m[1], ")") {
			if idx := strings.Index(part, "("); idx >= 0 {
				out.WriteString(unescapePDFString(part[idx+1:]))
				out.WriteString(" ")
			}
		}
	}

	// (text) ' and (text) "
	reQuote := regexp.MustCompile(`\(([^)]*)\)\s*['"]`)
	for _, m := range reQuote.FindAllStringSubmatch(content, -1) {
		out.WriteString(unescapePDFString(m[1]))
		out.WriteString(" ")
	}

	return out.String()
}

// unescapePDFString handles PDF string escapes.
func unescapePDFString(s string) string {
	s = strings.ReplaceAll(s, `\(`, "(")
	s = strings.ReplaceAll(s, `\)`, ")")
	s = strings.ReplaceAll(s, `\\`, "\\")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	s = strings.ReplaceAll(s, `\t`, "\t")
	// octal escapes \ddd
	re := regexp.MustCompile(`\\(\d{1,3})`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		var code int
		fmt.Sscanf(m, `\%d`, &code)
		if code > 0 && code < 256 {
			return string(rune(code))
		}
		return m
	})
}

// zlibInflate decompresses zlib/deflate data.
func zlibInflate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 10<<20))
}

// HasText reports whether the PDF contains extractable text (vs image-only).
func HasText(pdfBytes []byte) bool {
	_, err := ExtractTextFromPDF(pdfBytes)
	return err == nil
}

// OCRViaTesseract runs tesseract on the PDF/image as fallback for
// scanned documents. Returns extracted text or error if tesseract missing.
func OCRViaTesseract(pdfBytes []byte, lang string) (string, error) {
	if lang == "" {
		lang = "spa"
	}
	// write to temp file
	tmp, err := writeTemp(pdfBytes, ".pdf")
	if err != nil {
		return "", err
	}
	defer removeTemp(tmp)

	cmd := exec.Command("tesseract", tmp, "stdout", "-l", lang, "--psm", "6")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract falló (¿está instalado?): %v", err)
	}
	return string(out), nil
}

func writeTemp(data []byte, ext string) (string, error) {
	f, err := os.CreateTemp("", "facturable-*"+ext)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func removeTemp(path string) {
	_ = os.Remove(path)
	_ = os.Remove(filepath.Join(filepath.Dir(path), filepath.Base(path)))
}
