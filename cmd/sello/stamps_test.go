package main

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// endlessReader produces an unbounded stream of bytes: it lets the
// oversized-upload test cross the 50 MB cap without allocating the payload.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = '0'
	}
	return len(p), nil
}

func (endlessReader) Close() error { return nil }

// TestReadStampPDFEnforcesSizeCap: POST /v1/stamps must refuse an original
// PDF above the 50 MB cap with an explicit message (audit: the endpoint
// never validated the document size), never silently truncating it into a
// corrupt stamped document.
func TestReadStampPDFEnforcesSizeCap(t *testing.T) {
	t.Run("pdf under the cap is read whole", func(t *testing.T) {
		payload := "%PDF-1.4 small invoice"
		rec := httptest.NewRecorder()
		got, err := readStampPDF(rec, io.NopCloser(strings.NewReader(payload)))
		if err != nil {
			t.Fatalf("readStampPDF(): %v", err)
		}
		if string(got) != payload {
			t.Fatalf("payload altered: %q", got)
		}
	})
	t.Run("pdf above the cap is rejected with the audit message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		_, err := readStampPDF(rec, io.NopCloser(endlessReader{}))
		if !errors.Is(err, errStampPDFTooLarge) {
			t.Fatalf("error = %v, want errStampPDFTooLarge", err)
		}
		if want := "el PDF es demasiado grande (máximo 50MB)"; err.Error() != want {
			t.Fatalf("message = %q, want %q", err.Error(), want)
		}
	})
}
