package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// monthSpendSQL sums one user's LLM spend since the start of the current
// month (UTC). $1 is the user id; 0 sums every user.
const monthSpendSQL = `
	SELECT COALESCE((SELECT sum(cost_usd) FROM analyses
	                 WHERE ($1 = 0 OR user_id = $1) AND created_at >= date_trunc('month', now())), 0)
	     + COALESCE((SELECT sum(cost_usd) FROM llm_usage
	                 WHERE ($1 = 0 OR user_id = $1) AND created_at >= date_trunc('month', now())), 0)`

// Budget is one user's spending position this month.
type Budget struct {
	MonthlyUSD float64 `json:"monthly_usd"`
	SpentUSD   float64 `json:"spent_usd"`
}

// Remaining is the budget left this month (never negative).
func (b Budget) Remaining() float64 {
	return max(0, b.MonthlyUSD-b.SpentUSD)
}

// UserBudget returns a user's monthly budget and this month's spend.
func UserBudget(ctx context.Context, pool *pgxpool.Pool, userID int64) (Budget, error) {
	var b Budget
	if err := pool.QueryRow(ctx, `SELECT monthly_budget_usd::float8 FROM users WHERE id = $1`, userID).
		Scan(&b.MonthlyUSD); err != nil {
		return b, fmt.Errorf("load budget for user %d: %w", userID, err)
	}
	if err := pool.QueryRow(ctx, monthSpendSQL, userID).Scan(&b.SpentUSD); err != nil {
		return b, fmt.Errorf("load spend for user %d: %w", userID, err)
	}
	return b, nil
}

// TotalMonthSpend returns every user's combined spend this month.
func TotalMonthSpend(ctx context.Context, pool *pgxpool.Pool) (float64, error) {
	var spent float64
	err := pool.QueryRow(ctx, monthSpendSQL, int64(0)).Scan(&spent)
	return spent, err
}

// SetBudget changes a user's monthly budget.
func SetBudget(ctx context.Context, pool *pgxpool.Pool, userID int64, usd float64) error {
	_, err := pool.Exec(ctx, `UPDATE users SET monthly_budget_usd = $2 WHERE id = $1`, userID, usd)
	return err
}

// RecordLLMUsage stores the cost of a non-scoring LLM call for a user.
func RecordLLMUsage(ctx context.Context, pool *pgxpool.Pool, userID int64, kind, model string, prompt, cached, completion int64, costUSD float64) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO llm_usage (user_id, kind, model, prompt_tokens, cached_tokens, completion_tokens, cost_usd)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, userID, kind, model, prompt, cached, completion, costUSD)
	return err
}

// UserSpend is one row of the admin usage report.
type UserSpend struct {
	ID         int64      `json:"id"`
	Email      string     `json:"email"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastLogin  *time.Time `json:"last_login_at"`
	BudgetUSD  float64    `json:"monthly_budget_usd"`
	MonthUSD   float64    `json:"month_spend_usd"`
	Matches    int64      `json:"matches"`
	HasProfile bool       `json:"has_profile"`
}

// SpendByUser lists every user with this month's spend, for admins.
func SpendByUser(ctx context.Context, pool *pgxpool.Pool) ([]UserSpend, error) {
	rows, err := pool.Query(ctx, `
		SELECT u.id, u.email, u.name, u.created_at, u.last_login_at, u.monthly_budget_usd::float8,
		       COALESCE((SELECT sum(cost_usd) FROM analyses a
		                 WHERE a.user_id = u.id AND a.created_at >= date_trunc('month', now())), 0)::float8
		     + COALESCE((SELECT sum(cost_usd) FROM llm_usage l
		                 WHERE l.user_id = u.id AND l.created_at >= date_trunc('month', now())), 0)::float8,
		       (SELECT count(*) FROM user_jobs uj WHERE uj.user_id = u.id AND uj.stage = 'scored'),
		       EXISTS (SELECT 1 FROM profiles p WHERE p.user_id = u.id AND p.markdown <> '')
		FROM users u ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSpend
	for rows.Next() {
		var u UserSpend
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.LastLogin,
			&u.BudgetUSD, &u.MonthUSD, &u.Matches, &u.HasProfile); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
