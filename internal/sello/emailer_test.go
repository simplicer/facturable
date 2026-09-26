package sello

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

// fakeSMTP responde el diálogo mínimo EHLO/MAIL/RCPT/DATA/QUIT.
func fakeSMTP(t *testing.T, got *[]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		w("220 test ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			*got = append(*got, cmd)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250-test")
				w("250 STARTTLS")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 OK")
			case strings.HasPrefix(cmd, "DATA"):
				w("354 go")
				// consume hasta el punto final
				for {
					l, err := r.ReadString('\n')
					if err != nil || strings.TrimSpace(l) == "." {
						break
					}
				}
				w("250 queued")
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 OK")
			}
		}
	}()
	return ln.Addr().String()
}

func TestSendInvoiceEmailDialogoSMTP(t *testing.T) {
	var got []string
	addr := fakeSMTP(t, &got)
	cfg := SMTPConfig{Host: addr, From: "facturas@test.es"}
	pdf := []byte("%PDF-1.4 fake")
	// sin TLS en el fake: la conexión TLS fallará antes del diálogo; probamos
	// al menos la validación de argumentos y el mensaje de error TLS.
	err := SendInvoiceEmail(cfg, "cliente@test.es", "Factura R-1", pdf, "R-1.pdf")
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("se esperaba fallo de handshake TLS contra el fake (no-TLS), got: %v", err)
	}
	if err := SendInvoiceEmail(SMTPConfig{}, "", "s", pdf, "x.pdf"); err == nil {
		t.Fatal("config vacía aceptada")
	}
}
