-- +goose Up
-- When a company's whole board was last listed. Paged sources (Workday)
-- list only new postings on most runs and do a full sweep, which is what
-- detects closed postings, once a day.
ALTER TABLE companies ADD COLUMN last_full_fetch_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE companies DROP COLUMN last_full_fetch_at;
