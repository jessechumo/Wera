-- +goose Up
-- Chrome extension support.

-- Long-lived, revocable bearer tokens for the extension (one per device).
-- Only a hash is stored; the token is shown to the extension once.
CREATE TABLE api_tokens (
  id           BIGSERIAL PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ
);
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

-- What an application form asks that the profile does not hold: phone,
-- links, address, work authorization answers, education, EEO choices.
CREATE TABLE user_applicant (
  user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  details    JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Jobs a user added themselves (from the extension). They belong to that
-- user only: never rule-filtered, never shown to anyone else.
ALTER TABLE jobs ADD COLUMN added_by BIGINT REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX jobs_added_by_idx ON jobs (added_by) WHERE added_by IS NOT NULL;
CREATE UNIQUE INDEX jobs_manual_url_idx ON jobs (added_by, url) WHERE added_by IS NOT NULL;

-- +goose Down
DROP INDEX jobs_manual_url_idx;
DROP INDEX jobs_added_by_idx;
ALTER TABLE jobs DROP COLUMN added_by;
DROP TABLE user_applicant;
DROP TABLE api_tokens;
