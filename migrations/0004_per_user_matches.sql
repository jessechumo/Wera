-- +goose Up
-- Multi-user matching. Jobs stay global (fetched once for everyone); the
-- rule-filter outcome, score link and application status become per user.
-- Analyses stay keyed by (job, kind, profile_hash), so users whose profile
-- text is identical share scores instead of paying twice.

-- Each user's profile: the markdown the LLM scores against and the
-- preferences the rule filter applies (config.Preferences as JSON).
CREATE TABLE profiles (
  user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  markdown     TEXT NOT NULL DEFAULT '',
  profile_hash TEXT NOT NULL DEFAULT '',     -- sha256(markdown)
  preferences  JSONB NOT NULL DEFAULT '{}',
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per (user, job) the rules have looked at.
CREATE TABLE user_jobs (
  user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id             BIGINT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  content_hash       TEXT NOT NULL,            -- jobs.content_hash the rules saw
  stage              TEXT NOT NULL
                     CHECK (stage IN ('excluded','pending_score','scored','score_failed')),
  matched_categories TEXT[] NOT NULL DEFAULT '{}',
  exclude_reason     TEXT,
  exclude_evidence   TEXT,
  flags              TEXT[] NOT NULL DEFAULT '{}',
  analysis_id        BIGINT REFERENCES analyses(id) ON DELETE SET NULL,
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, job_id)
);
CREATE INDEX user_jobs_stage_idx ON user_jobs (user_id, stage);
CREATE INDEX user_jobs_job_idx ON user_jobs (job_id);

-- Who paid for each analysis (budgets); shared rows keep the first payer.
ALTER TABLE analyses ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX analyses_user_created_idx ON analyses (user_id, created_at);

ALTER TABLE applications ADD COLUMN user_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

-- Existing single-user data moves to an owner account. It has no password
-- yet: set the real email and a password with `wera users update` and
-- `wera users passwd`.
-- +goose StatementBegin
DO $$
DECLARE owner_id BIGINT;
BEGIN
  IF EXISTS (SELECT 1 FROM jobs) OR EXISTS (SELECT 1 FROM applications) THEN
    INSERT INTO users (email, name, is_admin) VALUES ('owner@wera.local', 'Owner', true)
    RETURNING id INTO owner_id;

    -- The rules that produced the existing results (the original roles.yaml).
    INSERT INTO profiles (user_id, profile_hash, preferences)
    SELECT owner_id,
           COALESCE((SELECT profile_hash FROM analyses ORDER BY created_at DESC, id DESC LIMIT 1), ''),
           '{"role_families":["sre","platform","devops","infrastructure","swe_infra","production","trading_ops","ml_infra","early_career"],
             "levels":["entry","mid"],"max_years_required":3,"us_only":true,"needs_sponsorship":true}';

    INSERT INTO user_jobs (user_id, job_id, content_hash, stage, matched_categories,
                           exclude_reason, exclude_evidence, flags, analysis_id)
    SELECT owner_id, j.id, j.content_hash, j.stage, j.matched_categories,
           j.exclude_reason, j.exclude_evidence, j.flags,
           (SELECT a.id FROM analyses a WHERE a.job_id = j.id AND a.kind = 'score'
            ORDER BY a.created_at DESC, a.id DESC LIMIT 1)
    FROM jobs j
    WHERE j.stage <> 'new';

    UPDATE analyses SET user_id = owner_id;
    UPDATE applications SET user_id = owner_id;
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE applications DROP CONSTRAINT applications_pkey;
ALTER TABLE applications ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE applications ADD PRIMARY KEY (user_id, job_id);
CREATE INDEX applications_job_idx ON applications (job_id);

-- Rule outcomes now live in user_jobs.
ALTER TABLE jobs DROP COLUMN stage;
ALTER TABLE jobs DROP COLUMN matched_categories;
ALTER TABLE jobs DROP COLUMN exclude_reason;
ALTER TABLE jobs DROP COLUMN exclude_evidence;
ALTER TABLE jobs DROP COLUMN flags;

-- +goose Down
-- Restores the single-user columns from the owner account (or the first
-- admin); other users' data is dropped.
ALTER TABLE jobs ADD COLUMN stage TEXT NOT NULL DEFAULT 'new'
  CHECK (stage IN ('new','excluded','pending_score','scored','score_failed'));
ALTER TABLE jobs ADD COLUMN matched_categories TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE jobs ADD COLUMN exclude_reason TEXT;
ALTER TABLE jobs ADD COLUMN exclude_evidence TEXT;
ALTER TABLE jobs ADD COLUMN flags TEXT[] NOT NULL DEFAULT '{}';
CREATE INDEX jobs_stage_idx ON jobs (stage);
UPDATE jobs j SET stage = uj.stage, matched_categories = uj.matched_categories,
                  exclude_reason = uj.exclude_reason, exclude_evidence = uj.exclude_evidence,
                  flags = uj.flags
FROM user_jobs uj
WHERE uj.job_id = j.id
  AND uj.user_id = (SELECT id FROM users WHERE is_admin ORDER BY id LIMIT 1);
DELETE FROM applications WHERE user_id <> (SELECT id FROM users WHERE is_admin ORDER BY id LIMIT 1);
ALTER TABLE applications DROP CONSTRAINT applications_pkey;
ALTER TABLE applications DROP COLUMN user_id;
ALTER TABLE applications ADD PRIMARY KEY (job_id);
ALTER TABLE analyses DROP COLUMN user_id;
DROP TABLE user_jobs;
DROP TABLE profiles;
