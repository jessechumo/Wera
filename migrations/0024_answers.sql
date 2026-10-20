-- +goose Up
-- What the user answered on application forms, so the next form with the
-- same question is filled the same way (on any browser they connect).
-- question_key is the question normalized (case, punctuation, "required").
CREATE TABLE application_answers (
  id           BIGSERIAL PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  question     TEXT NOT NULL,
  question_key TEXT NOT NULL,
  answer       TEXT NOT NULL,
  kind         TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'long', 'choice')),
  company      TEXT NOT NULL DEFAULT '',
  uses         INT NOT NULL DEFAULT 1,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, question_key)
);

-- +goose Down
DROP TABLE application_answers;
