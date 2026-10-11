-- +goose Up
ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ;

-- Single-use links sent by email (confirming an address, resetting a
-- password). Only a hash of the token is kept.
CREATE TABLE email_tokens (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose     TEXT NOT NULL CHECK (purpose IN ('verify', 'reset')),
  token_hash  TEXT NOT NULL UNIQUE,
  email       TEXT NOT NULL,          -- the address it was sent to
  expires_at  TIMESTAMPTZ NOT NULL,
  used_at     TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_tokens_user_idx ON email_tokens (user_id, purpose);

-- Email waiting to go out, and a record of what went out.
CREATE TABLE email_outbox (
  id               BIGSERIAL PRIMARY KEY,
  user_id          BIGINT REFERENCES users(id) ON DELETE SET NULL,
  to_addr          TEXT NOT NULL,
  tag              TEXT NOT NULL,          -- template name
  subject          TEXT NOT NULL,
  html             TEXT NOT NULL,
  text             TEXT NOT NULL,
  headers          JSONB NOT NULL DEFAULT '{}',
  status           TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sending', 'sent', 'failed')),
  attempts         INT NOT NULL DEFAULT 0,
  next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error       TEXT,
  provider_id      TEXT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at          TIMESTAMPTZ
);
CREATE INDEX email_outbox_due_idx ON email_outbox (next_attempt_at) WHERE status = 'queued';
CREATE INDEX email_outbox_user_idx ON email_outbox (user_id, tag, created_at DESC);

-- +goose Down
DROP TABLE email_outbox;
DROP TABLE email_tokens;
ALTER TABLE users DROP COLUMN email_verified_at;
