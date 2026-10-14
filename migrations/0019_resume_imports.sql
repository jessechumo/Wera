-- +goose Up
-- The resume Wera builds in the background from a fresh upload, so the
-- Resume tab opens on a finished resume instead of starting the import.
CREATE TABLE resume_imports (
  user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed')),
  error        TEXT NOT NULL DEFAULT '',
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE resume_imports;
