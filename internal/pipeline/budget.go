package pipeline

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/store"
)

// Allowance returns how much one user may spend on inference right now:
// the smallest of the per-run cap, what is left of their monthly budget,
// and what is left of the global monthly cap. why names the binding limit
// when the allowance is zero.
func Allowance(ctx context.Context, pool *pgxpool.Pool, env *config.Env, userID int64) (usd float64, why string, err error) {
	b, err := store.UserBudget(ctx, pool, userID)
	if err != nil {
		return 0, "", err
	}
	total, err := store.TotalMonthSpend(ctx, pool)
	if err != nil {
		return 0, "", err
	}
	usd, why = env.MaxCostPerRunUSD, "per-run cap"
	if r := b.Remaining(); r < usd {
		usd, why = r, "monthly budget used up"
	}
	if r := env.MaxMonthlyCostUSD - total; r < usd {
		usd, why = max(0, r), "global monthly cap reached"
	}
	return usd, why, nil
}
