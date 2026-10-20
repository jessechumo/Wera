-- +goose Up
-- Job boards found through jobs users added from a page Wera does not
-- watch: fetched every run alongside companies.yaml. added_at marks them
-- (added_by is who found the board, kept only while that user exists).
ALTER TABLE companies ADD COLUMN added_at TIMESTAMPTZ;
ALTER TABLE companies ADD COLUMN added_by BIGINT REFERENCES users(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE companies DROP COLUMN added_by;
ALTER TABLE companies DROP COLUMN added_at;
