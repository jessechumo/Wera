package mail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Log "sends" by logging the message and, with Dir set, writing it as an
// .eml file (open it in any mail app) plus the HTML part for a browser.
type Log struct {
	Dir  string
	From string
	Logf func(msg string, args ...any)
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Send writes the message to the log (and MAIL_LOG_DIR) instead of
// delivering it, and returns its Message-ID.
func (l *Log) Send(_ context.Context, m Message) (string, error) {
	raw, id, err := Build(l.From, "", m, time.Now())
	if err != nil {
		return "", err
	}
	if l.Logf != nil {
		l.Logf("email (log provider, not sent)", "to", m.To, "subject", m.Subject, "tag", m.Tag)
	}
	if l.Dir != "" {
		if err := os.MkdirAll(l.Dir, 0o750); err != nil {
			return "", err
		}
		base := filepath.Join(l.Dir, fmt.Sprintf("%s-%s-%s", time.Now().Format("20060102-150405"), m.Tag, unsafeName.ReplaceAllString(m.To, "_")))
		if err := os.WriteFile(base+".eml", raw, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(base+".html", []byte(m.HTML), 0o600); err != nil {
			return "", err
		}
	}
	return id, nil
}
