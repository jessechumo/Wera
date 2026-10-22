package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"wera/internal/store"
	"wera/internal/testutil"
)

// A new user's self-identification is unanswered, never "decline".
func TestApplicantSelfIDStartsUnanswered(t *testing.T) {
	pool := testutil.Pool(t)
	u := testutil.User(t, pool, false)
	a, saved, err := store.GetApplicant(context.Background(), pool, u)
	if err != nil || saved {
		t.Fatalf("defaults: %v saved=%v", err, saved)
	}
	if a.Gender != "" || a.Race != "" || a.Veteran != "" || a.Disability != "" || a.Hispanic != "" || a.Transgender != "" || a.SexualOrientation != "" {
		t.Errorf("self-identification must start unanswered: %+v", a)
	}
}

// Migration 0027 clears the "decline" defaults the extension saved back,
// and leaves real answers alone.
func TestMigrationClearsSavedDeclineDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Pool(t)
	u := testutil.User(t, pool, false)
	if err := store.SaveApplicant(ctx, pool, u.ID, &store.Applicant{FirstName: "Ada", Gender: "decline", Race: "White", Veteran: "decline", Disability: "No, I do not have a disability"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/0027_self_id_unset.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(strings.SplitN(string(raw), "-- +goose Up", 2)[1], "-- +goose Down")[0]
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	a, _, err := store.GetApplicant(ctx, pool, u)
	if err != nil {
		t.Fatal(err)
	}
	if a.Gender != "" || a.Veteran != "" || a.Race != "White" || a.Disability != "No, I do not have a disability" || a.FirstName != "Ada" {
		t.Errorf("after the migration: %+v", a)
	}
}
