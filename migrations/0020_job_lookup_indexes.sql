-- +goose Up
-- The extension looks the open page up on every visit: by address (with or
-- without a trailing slash), by the board's job id, or by an id inside the
-- address. These keep that off a scan of every job.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX jobs_url_trim_idx ON jobs (rtrim(url, '/'));
CREATE INDEX jobs_ext_id_idx ON jobs (ext_id);
CREATE INDEX jobs_url_trgm_idx ON jobs USING gin (url gin_trgm_ops);

-- +goose Down
DROP INDEX jobs_url_trgm_idx;
DROP INDEX jobs_ext_id_idx;
DROP INDEX jobs_url_trim_idx;
