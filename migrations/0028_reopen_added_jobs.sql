-- +goose Up
-- A job a user added is filed under the company when Wera watches it, and
-- the company's next fetch closed it because the board did not list it.
-- Nothing else closes an added job before its 45-day lifetime, so the ones
-- closed sooner than that were closed by this.
UPDATE jobs SET closed_at = NULL
WHERE source = 'manual' AND closed_at IS NOT NULL
  AND first_seen_at > now() - interval '45 days';

-- +goose Down
SELECT 1;
