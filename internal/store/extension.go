package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIToken is a connected browser extension (the token itself is never
// stored or returned again).
type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// CreateAPIToken stores a token hash for the user.
func CreateAPIToken(ctx context.Context, pool *pgxpool.Pool, userID int64, tokenHash, name string) (*APIToken, error) {
	t := &APIToken{Name: name}
	err := pool.QueryRow(ctx, `
		INSERT INTO api_tokens (user_id, token_hash, name) VALUES ($1, $2, $3)
		RETURNING id, created_at`, userID, tokenHash, name).Scan(&t.ID, &t.CreatedAt)
	return t, err
}

// APITokenUser returns the token's user and records its use (at most once
// a minute, to keep lookups read-mostly). pgx.ErrNoRows when unknown.
func APITokenUser(ctx context.Context, pool *pgxpool.Pool, tokenHash string) (*User, error) {
	var id int64
	var last *time.Time
	if err := pool.QueryRow(ctx, `SELECT user_id, last_used_at FROM api_tokens WHERE token_hash = $1`,
		tokenHash).Scan(&id, &last); err != nil {
		return nil, err
	}
	if last == nil || time.Since(*last) > time.Minute {
		if _, err := pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, tokenHash); err != nil {
			return nil, err
		}
	}
	return scanUser(pool.QueryRow(ctx, `SELECT id, email, name, is_admin, created_at FROM users WHERE id = $1`, id))
}

// ListAPITokens lists the user's connected extensions, newest first.
func ListAPITokens(ctx context.Context, pool *pgxpool.Pool, userID int64) ([]APIToken, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, created_at, last_used_at FROM api_tokens
		WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes one of the user's tokens (pgx.ErrNoRows if none).
func DeleteAPIToken(ctx context.Context, pool *pgxpool.Pool, userID, id int64) error {
	tag, err := pool.Exec(ctx, `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteAPITokenByHash revokes the token making the request (sign out).
func DeleteAPITokenByHash(ctx context.Context, pool *pgxpool.Pool, tokenHash string) error {
	_, err := pool.Exec(ctx, `DELETE FROM api_tokens WHERE token_hash = $1`, tokenHash)
	return err
}

// Applicant holds what application forms ask beyond the profile. Every
// field is optional; the extension fills only what is known.
type Applicant struct {
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	PreferredName   string `json:"preferred_name"`
	Email           string `json:"email"`
	Phone           string `json:"phone"`
	Address         string `json:"address"`
	City            string `json:"city"`
	State           string `json:"state"`
	PostalCode      string `json:"postal_code"`
	Country         string `json:"country"`
	LinkedIn        string `json:"linkedin"`
	GitHub          string `json:"github"`
	Portfolio       string `json:"portfolio"`
	CurrentCompany  string `json:"current_company"`
	CurrentTitle    string `json:"current_title"`
	YearsExperience string `json:"years_experience"`
	School          string `json:"school"`
	Degree          string `json:"degree"`
	Major           string `json:"major"`
	GraduationYear  string `json:"graduation_year"`
	GPA             string `json:"gpa"`
	// Work authorization answers, as forms phrase them: "yes" or "no".
	AuthorizedToWork  string `json:"authorized_to_work"`
	NeedsSponsorship  string `json:"needs_sponsorship"`
	WillingToRelocate string `json:"willing_to_relocate"`
	SalaryExpectation string `json:"salary_expectation"`
	StartDate         string `json:"start_date"`
	HowHeard          string `json:"how_heard"`
	// Voluntary self-identification; "decline" unless the user chooses.
	Gender     string `json:"gender"`
	Pronouns   string `json:"pronouns"`
	Race       string `json:"race"`
	Hispanic   string `json:"hispanic"`
	Veteran    string `json:"veteran"`
	Disability string `json:"disability"`
}

// Validate bounds the lengths and the yes/no answers.
func (a *Applicant) Validate() error {
	raw, _ := json.Marshal(a)
	var m map[string]string
	_ = json.Unmarshal(raw, &m)
	for k, v := range m {
		if len(v) > 300 {
			return fmt.Errorf("%s is too long", k)
		}
	}
	for k, v := range map[string]string{"authorized_to_work": a.AuthorizedToWork, "needs_sponsorship": a.NeedsSponsorship,
		"willing_to_relocate": a.WillingToRelocate} {
		if v != "" && v != "yes" && v != "no" {
			return fmt.Errorf("%s must be yes or no", k)
		}
	}
	return nil
}

// GetApplicant returns the saved details, or defaults from the account
// and profile (name, email, title, work authorization) when none are saved.
func GetApplicant(ctx context.Context, pool *pgxpool.Pool, user *User) (*Applicant, bool, error) {
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT details FROM user_applicant WHERE user_id = $1`, user.ID).Scan(&raw)
	if err == nil {
		a := &Applicant{}
		if err := json.Unmarshal(raw, a); err != nil {
			return nil, false, err
		}
		return a, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	a := &Applicant{Email: user.Email, HowHeard: "Company website",
		Gender: "decline", Race: "decline", Hispanic: "decline", Veteran: "decline", Disability: "decline"}
	if f := strings.Fields(user.Name); len(f) > 0 {
		a.FirstName, a.LastName = f[0], strings.Join(f[1:], " ")
	}
	var answers []byte
	if err := pool.QueryRow(ctx, `SELECT answers FROM profiles WHERE user_id = $1`, user.ID).Scan(&answers); err == nil {
		var ans struct {
			CurrentTitle      string `json:"current_title"`
			WorkAuthorization string `json:"work_authorization"`
		}
		_ = json.Unmarshal(answers, &ans)
		a.CurrentTitle = ans.CurrentTitle
		switch ans.WorkAuthorization {
		case "citizen_or_resident":
			a.AuthorizedToWork, a.NeedsSponsorship = "yes", "no"
		case "sponsorship_future":
			a.AuthorizedToWork, a.NeedsSponsorship = "yes", "yes"
		case "sponsorship_now":
			a.AuthorizedToWork, a.NeedsSponsorship = "no", "yes"
		}
	}
	return a, false, nil
}

// SaveApplicant stores the user's details.
func SaveApplicant(ctx context.Context, pool *pgxpool.Pool, userID int64, a *Applicant) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO user_applicant (user_id, details) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET details = EXCLUDED.details, updated_at = now()`, userID, raw)
	return err
}

// ManualJob is a job a user adds from a web page.
type ManualJob struct {
	URL         string
	Company     string
	Title       string
	Location    string
	Remote      *bool
	Department  string
	Description string
	Industry    string
}

// NormalizeJobURL drops the fragment, tracking parameters, a trailing
// slash and the case of the host, so the same posting matches itself.
func NormalizeJobURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") || k == "ref" || k == "source" || k == "src" || k == "gh_src" || k == "lever-source" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}

// AddManualJob creates (or finds) the user's job for this URL, links it to
// them as a pending match saved in their tracker, and returns its id and
// whether it was new. The company is matched by name to a watched company
// when one exists; otherwise a private "manual" company is created.
func AddManualJob(ctx context.Context, pool *pgxpool.Pool, userID int64, in ManualJob) (int64, bool, error) {
	in.URL = NormalizeJobURL(in.URL)
	if id, err := LookupJobByURL(ctx, pool, userID, in.URL); err == nil {
		return id, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	industry := in.Industry
	if industry == "" {
		industry = "other"
	}
	var companyID int64
	err = tx.QueryRow(ctx, `SELECT id FROM companies WHERE lower(name) = lower($1) ORDER BY ats = 'manual', id LIMIT 1`,
		in.Company).Scan(&companyID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO companies (name, ats, token, industry, enabled)
			VALUES ($1, 'manual', 'manual:' || lower($1), $2, false)
			ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`, in.Company, industry).Scan(&companyID)
	}
	if err != nil {
		return 0, false, fmt.Errorf("company: %w", err)
	}

	sum := sha256.Sum256([]byte(in.URL))
	extID := fmt.Sprintf("u%d-%s", userID, hex.EncodeToString(sum[:8]))
	h := sha256.Sum256([]byte(in.Title + "\x00" + in.Location + "\x00" + in.Description))
	contentHash := hex.EncodeToString(h[:])
	var jobID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO jobs (company_id, source, ext_id, title, location_raw, is_remote, url, department,
		                  description, content_hash, added_by)
		VALUES ($1, 'manual', $2, $3, NULLIF($4, ''), $5, $6, NULLIF($7, ''), $8, $9, $10)
		RETURNING id`, companyID, extID, in.Title, in.Location, in.Remote, in.URL, in.Department,
		in.Description, contentHash, userID).Scan(&jobID); err != nil {
		return 0, false, fmt.Errorf("insert job: %w", err)
	}
	// Added on purpose, so never rule-filtered: straight to the scoring queue
	// and into the tracker as saved.
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_jobs (user_id, job_id, content_hash, stage) VALUES ($1, $2, $3, 'pending_score')`,
		userID, jobID, contentHash); err != nil {
		return 0, false, fmt.Errorf("link job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO applications (user_id, job_id, status, notes) VALUES ($1, $2, 'saved', '')
		ON CONFLICT DO NOTHING`, userID, jobID); err != nil {
		return 0, false, fmt.Errorf("save to tracker: %w", err)
	}
	return jobID, true, tx.Commit(ctx)
}

var trailingID = regexp.MustCompile(`([0-9]{5,}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/?(?:apply|application)?/?$`)

// LookupJobByURL finds one of the user's jobs for a page URL: the same
// normalized URL, or a posting whose board id ends the URL path (apply
// pages often live on a different host than the listing).
func LookupJobByURL(ctx context.Context, pool *pgxpool.Pool, userID int64, raw string) (int64, error) {
	norm := NormalizeJobURL(raw)
	var id int64
	err := pool.QueryRow(ctx, `
		SELECT j.id FROM user_jobs uj JOIN jobs j ON j.id = uj.job_id
		WHERE uj.user_id = $1 AND (j.url = $2 OR j.url = $3 OR rtrim(j.url, '/') = $2)
		ORDER BY j.closed_at IS NULL DESC LIMIT 1`, userID, norm, strings.TrimSpace(raw)).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	u, perr := url.Parse(norm)
	if perr != nil {
		return 0, pgx.ErrNoRows
	}
	m := trailingID.FindStringSubmatch(u.Path)
	if m == nil {
		return 0, pgx.ErrNoRows
	}
	err = pool.QueryRow(ctx, `
		SELECT j.id FROM user_jobs uj JOIN jobs j ON j.id = uj.job_id
		WHERE uj.user_id = $1 AND (j.ext_id = $2 OR j.url LIKE '%' || $2 || '%')
		ORDER BY j.closed_at IS NULL DESC LIMIT 1`, userID, m[1]).Scan(&id)
	return id, err
}
