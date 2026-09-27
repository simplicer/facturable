package sello

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPConfig describes the outgoing relay for invoice delivery.
type SMTPConfig struct {
	Host     string // host:port
	User     string
	Password string
	From     string
}

// SendInvoiceEmail delivers the stamped PDF to the customer. STARTTLS is
// required by the server; plain relays are refused.
func SendInvoiceEmail(cfg SMTPConfig, to, subject string, pdf []byte, filename string) error {
	if cfg.Host == "" || cfg.From == "" || to == "" {
		return fmt.Errorf("falta configuración SMTP o destinatario")
	}
	host := cfg.Host
	if i := strings.LastIndex(cfg.Host, ":"); i > 0 {
		host = cfg.Host[:i]
	}
	conn, err := net.Dial("tcp", cfg.Host)
	if err != nil {
		return fmt.Errorf("no se pudo conectar al servidor de correo: %w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return fmt.Errorf("el servidor de correo no ofrece STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("STARTTLS rechazado: %w", err)
	}
	if cfg.User != "" {
		auth := smtp.PlainAuth("", cfg.User, cfg.Password, host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("autenticación SMTP rechazada: %w", err)
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	boundary := "facturable-pdf-boundary"
	headers := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=%s\r\n\r\n", cfg.From, to, subject, boundary)
	fmt.Fprintf(w, "%s", headers)
	fmt.Fprintf(w, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary, subject)
	fmt.Fprintf(w, "--%s\r\nContent-Type: application/pdf; name=%q\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=%q\r\n\r\n",
		boundary, filename, filename)
	// base64 en líneas de 76 chars (RFC 2045)
	enc := base64Lines(pdf)
	fmt.Fprintf(w, "%s\r\n", enc)
	fmt.Fprintf(w, "--%s--\r\n", boundary)
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// base64Lines encodes in 76-char lines (RFC 2045).
func base64Lines(b []byte) string {
	enc := base64.StdEncoding
	s := enc.EncodeToString(b)
	var out strings.Builder
	for len(s) > 76 {
		out.WriteString(s[:76])
		out.WriteString("\r\n")
		s = s[76:]
	}
	out.WriteString(s)
	return out.String()
}
