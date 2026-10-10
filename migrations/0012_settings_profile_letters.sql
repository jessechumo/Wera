-- +goose Up
-- Per-user settings (theme, notification preferences, ...) as JSON so new
-- options need no migration.
ALTER TABLE users ADD COLUMN settings JSONB NOT NULL DEFAULT '{}';

-- Companies a user never wants to see; their jobs are hidden and skipped
-- by scoring (no LLM cost).
CREATE TABLE user_hidden_companies (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  company_id BIGINT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, company_id)
);

-- Profile picture, re-encoded server side (never the uploaded bytes).
CREATE TABLE avatars (
  user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  data         BYTEA NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The uploaded resume PDF, so users can view what they gave us.
CREATE TABLE resume_files (
  user_id     BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  filename    TEXT NOT NULL,
  data        BYTEA NOT NULL,
  uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Generated (and possibly edited) cover letters, one per user and job.
CREATE TABLE cover_letters (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id     BIGINT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  model      TEXT NOT NULL,
  edited     BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, job_id)
);

-- +goose Down
DROP TABLE cover_letters;
DROP TABLE resume_files;
DROP TABLE avatars;
DROP TABLE user_hidden_companies;
ALTER TABLE users DROP COLUMN settings;
