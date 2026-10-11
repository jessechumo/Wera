package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"
)

// SMTP sends through an SMTP server with STARTTLS (port 587), implicit
// TLS (port 465), or plain SMTP for a local relay.
type SMTP struct {
	Config Config
	// Dial is for tests; nil uses the network.
	Dial func(ctx context.Context, addr string) (net.Conn, error)
}

func (s *SMTP) Send(ctx context.Context, m Message) (string, error) {
	c := s.Config
	raw, id, err := Build(c.From, c.ReplyTo, m, time.Now())
	if err != nil {
		return "", err
	}
	from, _ := mail.ParseAddress(c.From)
	to, _ := mail.ParseAddress(m.To)
	addr := net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dial := s.Dial
	if dial == nil {
		d := &net.Dialer{}
		dial = func(ctx context.Context, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tlsConf := &tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12}
	if c.SMTPSecurity == "tls" {
		conn = tls.Client(conn, tlsConf)
	}
	client, err := smtp.NewClient(conn, c.SMTPHost)
	if err != nil {
		conn.Close()
		return "", fmt.Errorf("smtp hello: %w", err)
	}
	defer client.Close()
	if c.SMTPSecurity == "" || c.SMTPSecurity == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return "", fmt.Errorf("%s does not offer STARTTLS; set SMTP_SECURITY=tls for port 465", c.SMTPHost)
		}
		if err := client.StartTLS(tlsConf); err != nil {
			return "", fmt.Errorf("starttls: %w", err)
		}
	}
	if c.SMTPUsername != "" {
		// PlainAuth refuses to send credentials over an unencrypted
		// connection to anything but localhost.
		if err := client.Auth(smtp.PlainAuth("", c.SMTPUsername, c.SMTPPassword, c.SMTPHost)); err != nil {
			return "", fmt.Errorf("smtp login: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return "", fmt.Errorf("smtp from: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return "", fmt.Errorf("smtp to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return "", fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return "", fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("smtp send: %w", err)
	}
	_ = client.Quit()
	return id, nil
}
