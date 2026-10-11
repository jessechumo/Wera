// Package mail sends Wera's email: rendered templates (HTML with a plain
// text alternative) delivered through a provider. Nothing is sent unless
// a provider is configured (MAIL_PROVIDER); "log" writes messages to the
// log and a folder for development, "smtp" works with any provider that
// offers SMTP (Google Workspace, Zoho, Amazon SES, Resend, Postmark...).
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Message is one email ready to send.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
	// Headers adds headers such as List-Unsubscribe.
	Headers map[string]string
	// Tag names the template, for logs and provider analytics.
	Tag string
}

// Mailer delivers messages. It returns the provider's message id when
// there is one.
type Mailer interface {
	Send(ctx context.Context, m Message) (string, error)
}

// Config is how mail is sent. An empty Provider disables email.
type Config struct {
	Provider  string // "", "log" or "smtp"
	From      string // "Wera <hello@example.com>"
	ReplyTo   string
	PublicURL string // the dashboard's address, for links: https://wera.example.com
	LogDir    string // "log" provider: also write each message here as .eml

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPSecurity string // "starttls" (default, port 587), "tls" (port 465) or "none" (local relays only)
}

// Enabled reports whether email is set up.
func (c Config) Enabled() bool { return c.Provider != "" }

// ErrDisabled means no provider is configured.
var ErrDisabled = errors.New("email is not set up on this server")

// Validate checks the settings for the chosen provider.
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	var errs []string
	if _, err := mail.ParseAddress(c.From); err != nil {
		errs = append(errs, "MAIL_FROM must be an address like \"Wera <hello@example.com>\"")
	}
	if c.ReplyTo != "" {
		if _, err := mail.ParseAddress(c.ReplyTo); err != nil {
			errs = append(errs, "MAIL_REPLY_TO is not a valid address")
		}
	}
	if !strings.HasPrefix(c.PublicURL, "https://") && !strings.HasPrefix(c.PublicURL, "http://") {
		errs = append(errs, "PUBLIC_URL must be the dashboard's address (https://...) so emails can link to it")
	}
	switch c.Provider {
	case "log":
	case "smtp":
		if c.SMTPHost == "" || c.SMTPPort == 0 {
			errs = append(errs, "SMTP_HOST and SMTP_PORT are required")
		}
		switch c.SMTPSecurity {
		case "", "starttls", "tls", "none":
		default:
			errs = append(errs, "SMTP_SECURITY must be starttls, tls or none")
		}
	default:
		errs = append(errs, fmt.Sprintf("MAIL_PROVIDER %q is not supported (use log or smtp)", c.Provider))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// New returns the configured Mailer, or ErrDisabled.
func New(c Config, logf func(msg string, args ...any)) (Mailer, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.Provider {
	case "smtp":
		return &SMTP{Config: c}, nil
	default:
		return &Log{Dir: c.LogDir, From: c.From, Logf: logf}, nil
	}
}
