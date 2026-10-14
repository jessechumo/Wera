-- +goose Up
-- What admins did in the admin app, for accountability: who changed which
-- account, moderated what, or started a run.
CREATE TABLE admin_audit (
  id           BIGSERIAL PRIMARY KEY,
  actor_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
  actor_email  TEXT NOT NULL,        -- kept when the admin's account is gone
  action       TEXT NOT NULL,        -- e.g. user.update, content.delete, run.trigger
  target       TEXT NOT NULL DEFAULT '',
  detail       JSONB NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_created_idx ON admin_audit (created_at DESC);

-- +goose Down
DROP TABLE admin_audit;
