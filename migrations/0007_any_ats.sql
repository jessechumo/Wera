-- +goose Up
-- More applicant tracking systems than the original three; the list of
-- valid ones lives in Go (config.KnownATS), checked when companies.yaml
-- loads, so the database no longer duplicates it.
ALTER TABLE companies DROP CONSTRAINT companies_ats_check;

-- +goose Down
ALTER TABLE companies ADD CONSTRAINT companies_ats_check
  CHECK (ats IN ('greenhouse','lever','ashby'));
