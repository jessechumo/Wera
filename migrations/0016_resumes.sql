-- +goose Up
-- Structured resumes: one base resume per user plus at most one tailored
-- copy per job. data is resume.Resume, layout is resume.Layout, fit is
-- the last one-page fit report.
CREATE TABLE resumes (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id     BIGINT REFERENCES jobs(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  data       JSONB NOT NULL,
  layout     JSONB NOT NULL DEFAULT '{}',
  fit        JSONB,
  notes      JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX resumes_base_idx ON resumes (user_id) WHERE job_id IS NULL;
CREATE UNIQUE INDEX resumes_job_idx ON resumes (user_id, job_id) WHERE job_id IS NOT NULL;

-- +goose Down
DROP TABLE resumes;
