-- +goose Up
-- ETag of each board's last successful fetch, sent as If-None-Match so an
-- unchanged board answers 304 with no body.
ALTER TABLE companies ADD COLUMN etag TEXT;

-- +goose Down
ALTER TABLE companies DROP COLUMN etag;
