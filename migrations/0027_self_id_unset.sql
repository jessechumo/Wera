-- +goose Up
-- Self-identification used to default to "decline", and the extension
-- saved those defaults back as if the user had chosen them, so forms got
-- "Prefer not to say" for people who never said so. Unanswered is now ""
-- (the extension asks once and remembers); clear the stored defaults.
UPDATE user_applicant
SET details = details
  || CASE WHEN details->>'gender' = 'decline' THEN '{"gender":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'race' = 'decline' THEN '{"race":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'hispanic' = 'decline' THEN '{"hispanic":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'veteran' = 'decline' THEN '{"veteran":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'disability' = 'decline' THEN '{"disability":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'transgender' = 'decline' THEN '{"transgender":""}'::jsonb ELSE '{}'::jsonb END
  || CASE WHEN details->>'sexual_orientation' = 'decline' THEN '{"sexual_orientation":""}'::jsonb ELSE '{}'::jsonb END;

-- +goose Down
SELECT 1;
