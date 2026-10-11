package notify

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"wera/internal/mail"
	"wera/internal/store"
	"wera/internal/testutil"
)

type flaky struct {
	fail bool
	sent []mail.Message
}

func (f *flaky) Send(_ context.Context, m mail.Message) (string, error) {
	if f.fail {
		return "", errors.New("relay said no")
	}
	f.sent = append(f.sent, m)
	return "<id@wera.test>", nil
}

func TestDeliverRetriesThenGivesUp(t *testing.T) {
	pool := testutil.Pool(t)
	ctx := context.Background()
	u := testutil.User(t, pool, false)
	m := &flaky{fail: true}
	s := &Service{Pool: pool, Mailer: m, Log: slog.Default(), Config: mail.Config{Provider: "log", From: "Wera <hello@wera.test>", PublicURL: "https://wera.test"}}
	if err := s.SendTest(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT max(id) FROM email_outbox WHERE to_addr = $1`, u.Email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM email_outbox WHERE id = $1`, id) })
	due := func() { pool.Exec(ctx, `UPDATE email_outbox SET next_attempt_at = now() WHERE id = $1`, id) }

	for attempt := 1; attempt <= store.MaxEmailAttempts; attempt++ {
		due()
		if _, err := s.Deliver(ctx, 50); err != nil {
			t.Fatal(err)
		}
		var status string
		var attempts int
		_ = pool.QueryRow(ctx, `SELECT status, attempts FROM email_outbox WHERE id = $1`, id).Scan(&status, &attempts)
		want := "queued"
		if attempt == store.MaxEmailAttempts {
			want = "failed"
		}
		if status != want || attempts != attempt {
			t.Fatalf("attempt %d: status %s, attempts %d", attempt, status, attempts)
		}
	}

	// A working provider: queued mail goes out once.
	m.fail = false
	pool.Exec(ctx, `UPDATE email_outbox SET status = 'queued', attempts = 0 WHERE id = $1`, id)
	due()
	if n, err := s.Deliver(ctx, 50); err != nil || n < 1 {
		t.Fatalf("deliver: %d %v", n, err)
	}
	if n, _ := s.Deliver(ctx, 50); n != 0 {
		t.Errorf("sent twice")
	}
	if len(m.sent) == 0 || m.sent[len(m.sent)-1].Subject != "Wera email is working" {
		t.Errorf("sent: %+v", m.sent)
	}
}
