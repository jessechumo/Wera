package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"sort"
	"strings"
	"time"
)

// header values never carry line breaks (no header injection).
func headerSafe(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Build writes the message as MIME: multipart/alternative with a
// quoted-printable text part and HTML part.
func Build(from, replyTo string, m Message, now time.Time) ([]byte, string, error) {
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return nil, "", fmt.Errorf("from: %w", err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return nil, "", fmt.Errorf("to: %w", err)
	}
	domain := fromAddr.Address[strings.LastIndex(fromAddr.Address, "@")+1:]
	id := fmt.Sprintf("<%s@%s>", randomHex(12), domain)
	boundary := "wera-" + randomHex(10)

	var b bytes.Buffer
	h := map[string]string{
		"From":         fromAddr.String(),
		"To":           to.String(),
		"Subject":      mime.QEncoding.Encode("utf-8", headerSafe(m.Subject)),
		"Date":         now.Format(time.RFC1123Z),
		"Message-ID":   id,
		"MIME-Version": "1.0",
		"Content-Type": `multipart/alternative; boundary="` + boundary + `"`,
	}
	if replyTo != "" {
		h["Reply-To"] = replyTo
	}
	for k, v := range m.Headers {
		h[k] = headerSafe(v)
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\r\n", headerSafe(k), h[k])
	}
	b.WriteString("\r\n")
	for _, part := range []struct{ typ, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.typ)
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n"))); err != nil {
			return nil, "", err
		}
		if err := qp.Close(); err != nil {
			return nil, "", err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), id, nil
}
