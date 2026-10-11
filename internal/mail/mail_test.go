package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestEveryTemplateRenders(t *testing.T) {
	for _, name := range Templates() {
		r, err := Render(name, Sample(name, "https://wera.example.com", "ada@example.com", "Ada Lovelace"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Subject == "" || !strings.Contains(r.HTML, "<!doctype html>") || len(r.Text) < 40 {
			t.Errorf("%s: incomplete: %q", name, r.Subject)
		}
		if strings.Contains(r.HTML, "ZgotmplZ") || strings.Contains(r.HTML, "<no value>") || strings.Contains(r.Text, "<no value>") {
			t.Errorf("%s: a value was rejected or missing", name)
		}
	}
	r, _ := Render("digest", Sample("digest", "https://wera.example.com", "a@b.co", "Ada"))
	if r.Subject != "3 new matches: Software Engineer, Infrastructure at Stripe" || !strings.Contains(r.HTML, "color:#1e7a3c") {
		t.Errorf("digest: %q", r.Subject)
	}
}

func TestRenderEscapesAndChecksTypes(t *testing.T) {
	d := Sample("match", "https://wera.example.com", "a@b.co", "Ada").(Match)
	d.Job.Title = `<script>alert(1)</script> & Co`
	d.Job.Company = "AT&T"
	d.Job.URL = "javascript:alert(1)"
	r, err := Render("match", d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<script>") || strings.Contains(r.HTML, `href="javascript:`) {
		t.Error("unescaped HTML or unsafe link in the email")
	}
	if !strings.Contains(r.Subject, "at AT&T") {
		t.Errorf("subject must be plain text: %q", r.Subject)
	}
	if _, err := Render("match", Verify{}); err == nil {
		t.Error("wrong data type must be an error")
	}
}

func TestBuildMIME(t *testing.T) {
	raw, id, err := Build("Wera <hello@wera.example.com>", "help@wera.example.com", Message{
		To: "Ada <ada@example.com>", Subject: "Résumé ready\r\nBcc: evil@x.com", HTML: "<p>Hi ✓</p>", Text: "Hi ✓",
		Headers: map[string]string{"List-Unsubscribe": "<https://wera.example.com/settings>"},
	}, time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" || msg.Header.Get("Message-Id") != id || msg.Header.Get("Reply-To") == "" {
		t.Errorf("headers: %v", msg.Header)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Résumé ready Bcc: evil@x.com" {
		t.Errorf("subject: %q", subject)
	}
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p) // the reader decodes quoted-printable
		types = append(types, p.Header.Get("Content-Type")+"="+string(body))
	}
	if len(types) != 2 || !strings.Contains(types[0], "text/plain") || !strings.HasSuffix(types[1], "<p>Hi ✓</p>") {
		t.Errorf("parts: %q", types)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (Config{}).Validate(); err != nil {
		t.Errorf("disabled config is valid: %v", err)
	}
	if _, err := New(Config{}, nil); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled: %v", err)
	}
	bad := Config{Provider: "smtp", From: "nope", PublicURL: "wera"}
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "SMTP_HOST") || !strings.Contains(err.Error(), "MAIL_FROM") {
		t.Errorf("bad config: %v", err)
	}
}

// A minimal SMTP server: records the envelope and the data.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var log strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			log.WriteString(strings.TrimSpace(line) + "\n")
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250-fake\r\n250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "AUTH"):
				w("235 ok")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 ok")
			case cmd == "DATA":
				w("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					log.WriteString(l)
				}
				w("250 queued")
			case cmd == "QUIT":
				w("221 bye")
				got <- log.String()
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestSMTPSend(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	s := &SMTP{Config: Config{Provider: "smtp", From: "Wera <hello@wera.example.com>", PublicURL: "https://wera.example.com",
		SMTPHost: "localhost", SMTPPort: p, SMTPSecurity: "none", SMTPUsername: "u", SMTPPassword: "p"},
		Dial: func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		}}
	r, _ := Render("test", Sample("test", "https://wera.example.com", "ada@example.com", "Ada"))
	id, err := s.Send(context.Background(), Message{To: "ada@example.com", Subject: r.Subject, HTML: r.HTML, Text: r.Text, Tag: "test"})
	if err != nil {
		t.Fatal(err)
	}
	log := <-got
	for _, want := range []string{"MAIL FROM:<hello@wera.example.com>", "RCPT TO:<ada@example.com>", "Subject: Wera email is working", id} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in the session:\n%s", want, log[:min(len(log), 600)])
		}
	}
}
