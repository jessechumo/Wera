-- +goose Up
-- Jobs a user added are shared with everyone they suit, so deleting the
-- account that added one must not delete it for the others.
ALTER TABLE jobs DROP CONSTRAINT jobs_added_by_fkey;
ALTER TABLE jobs ADD CONSTRAINT jobs_added_by_fkey FOREIGN KEY (added_by) REFERENCES users(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE jobs DROP CONSTRAINT jobs_added_by_fkey;
ALTER TABLE jobs ADD CONSTRAINT jobs_added_by_fkey FOREIGN KEY (added_by) REFERENCES users(id) ON DELETE CASCADE;
