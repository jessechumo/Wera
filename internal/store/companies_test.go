package store_test

import (
	"context"
	"testing"

	"wera/internal/config"
	"wera/internal/store"
	"wera/internal/testutil"
)

// A board companies.yaml starts listing under another name than the
// database has it (renamed, or first found through an added job) moves to
// the YAML entry instead of failing the sync on the unique board.
func TestSyncCompaniesHandsOverBoards(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Pool(t)
	oldID, oldName := testutil.Company(t, pool, "other")
	newName := testutil.Unique("Renamed")
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM companies WHERE name = $1`, newName) })

	ids, err := store.SyncCompanies(ctx, pool, []config.Company{{Name: newName, ATS: "greenhouse", Token: oldName, Industry: "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if ids[newName] != oldID {
		t.Errorf("renamed entry got company %d, want the old row %d", ids[newName], oldID)
	}

	// Both names exist: the jobs follow the board to the YAML entry.
	heldID, held := testutil.Company(t, pool, "other")
	takerID, taker := testutil.Company(t, pool, "other")
	jobs := testutil.Jobs(t, pool, heldID, testutil.Job{Title: "Charge Nurse"})
	ids, err = store.SyncCompanies(ctx, pool, []config.Company{{Name: taker, ATS: "greenhouse", Token: held, Industry: "other"}})
	if err != nil {
		t.Fatal(err)
	}
	if ids[taker] != takerID {
		t.Fatalf("got company %d, want %d", ids[taker], takerID)
	}
	var owner int64
	var ats string
	pool.QueryRow(ctx, `SELECT company_id FROM jobs WHERE id = $1`, jobs[0]).Scan(&owner)
	pool.QueryRow(ctx, `SELECT ats FROM companies WHERE id = $1`, heldID).Scan(&ats)
	if owner != takerID || ats != "manual" {
		t.Errorf("job owner %d (want %d), old row ats %q (want manual)", owner, takerID, ats)
	}
}
