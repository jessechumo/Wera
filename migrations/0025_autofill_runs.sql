-- +goose Up
-- Each application the extension helped fill: which job, which site, how
-- many pages and fields. Shown on the job in the dashboard.
CREATE TABLE autofill_runs (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id     BIGINT REFERENCES jobs(id) ON DELETE SET NULL,
  host       TEXT NOT NULL,
  pages      INT NOT NULL DEFAULT 1,
  filled     INT NOT NULL DEFAULT 0,
  suggested  INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX autofill_runs_user_job_idx ON autofill_runs (user_id, job_id, created_at DESC);

-- +goose Down
DROP TABLE autofill_runs;
