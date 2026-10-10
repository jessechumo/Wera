-- +goose Up
-- What the user gave at signup, kept so they can regenerate their profile:
-- the resume as plain text (the uploaded file itself is not stored) and the
-- questionnaire answers (profile.Answers as JSON).
ALTER TABLE profiles ADD COLUMN resume_text TEXT NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN answers JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE profiles DROP COLUMN answers;
ALTER TABLE profiles DROP COLUMN resume_text;
