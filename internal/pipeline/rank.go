package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/embed"
	"wera/internal/relevance"
	"wera/internal/store"
)

// embedChunk is how many postings are embedded and saved at a time, so a
// cut-short pass (a request deadline) keeps the work it finished.
const embedChunk = 256

// Embedder ranks jobs by embedding similarity. A nil *Embedder means no
// embedding endpoint is configured and ranking uses TF-IDF alone.
type Embedder struct {
	Client *embed.Client
	Model  string
	Log    *slog.Logger
}

// NewEmbedder returns the configured embedder, or nil.
func NewEmbedder(env *config.Env, log *slog.Logger) *Embedder {
	c := embed.New(env.EmbedBaseURL, env.EmbedModel, env.EmbedAPIKey)
	if c == nil {
		return nil
	}
	return &Embedder{Client: c, Model: env.EmbedModel, Log: log}
}

// RankPending re-ranks the user's unscored matches against their profile
// without an AI call, stores the estimates (35..85) and returns them. The
// estimates order the AI scoring queue, decide which jobs get an AI score
// under the daily limit, and preview jobs on the dashboard.
//
// Jobs with shared facts are ranked by embedding similarity, which tracks
// the AI's fit scores far better than word matching; the rest (new
// postings whose facts are not extracted yet) fall back to TF-IDF until
// they have facts.
func RankPending(ctx context.Context, pool *pgxpool.Pool, e *Embedder, prof *store.Profile) (map[int64]int, error) {
	docs, err := store.RankDocs(ctx, pool, prof.UserID)
	if err != nil {
		return nil, err
	}
	est := relevance.Rank(prof.Markdown, docs)
	if e != nil && len(docs) > 0 {
		ids := make([]int64, len(docs))
		for i, d := range docs {
			ids[i] = d.ID
		}
		sims, err := e.Similarities(ctx, pool, prof, ids)
		if err != nil {
			e.Log.Warn("embedding ranking unavailable; using word matching", "user_id", prof.UserID, "err", err)
		}
		for id, v := range relevance.Scale(sims) {
			est[id] = v
		}
	}
	return est, store.SetEstimates(ctx, pool, prof.UserID, est)
}

// Similarities returns the profile's similarity to each job that has
// facts, embedding the profile and any postings not embedded yet. On an
// error it still returns what it could compute.
func (e *Embedder) Similarities(ctx context.Context, pool *pgxpool.Pool, prof *store.Profile, jobIDs []int64) (map[int64]float64, error) {
	pv, err := store.ProfileEmbedding(ctx, pool, prof.UserID, prof.ProfileHash, e.Model)
	if err != nil {
		return nil, err
	}
	if pv == nil {
		vecs, err := e.Client.Embed(ctx, []string{embed.ProfileText(prof.Markdown)})
		if err != nil {
			return nil, fmt.Errorf("embed profile: %w", err)
		}
		pv = vecs[0]
		if err := store.SaveProfileEmbedding(ctx, pool, prof.UserID, prof.ProfileHash, e.Model, pv); err != nil {
			return nil, err
		}
	}
	_, embedErr := e.EmbedJobs(ctx, pool, jobIDs)
	vecs, err := store.JobEmbeddings(ctx, pool, jobIDs, e.Model)
	if err != nil {
		return nil, err
	}
	sims := make(map[int64]float64, len(vecs))
	for id, v := range vecs {
		sims[id] = embed.Cosine(pv, v)
	}
	return sims, embedErr
}

// EmbedJobs embeds those of the jobs that have facts but no current
// embedding, and returns how many it embedded.
func (e *Embedder) EmbedJobs(ctx context.Context, pool *pgxpool.Pool, jobIDs []int64) (int, error) {
	todo, err := store.JobsToEmbed(ctx, pool, jobIDs, e.Model)
	if err != nil {
		return 0, err
	}
	done := 0
	for start := 0; start < len(todo); start += embedChunk {
		part := todo[start:min(start+embedChunk, len(todo))]
		texts := make([]string, len(part))
		for i, c := range part {
			texts[i] = embed.JobText(c.Title, c.Facts.Seniority, c.Facts.SkillsRequired, c.Facts.Digest)
		}
		vecs, err := e.Client.Embed(ctx, texts)
		if err != nil {
			return done, fmt.Errorf("embed jobs: %w", err)
		}
		batch := make(map[int64][]float32, len(part))
		for i, c := range part {
			batch[c.ID] = vecs[i]
		}
		if err := store.SaveJobEmbeddings(ctx, pool, e.Model, batch); err != nil {
			return done, err
		}
		done += len(part)
	}
	return done, nil
}

// Backfill embeds open postings that have facts but no embedding, for at
// most maxTime, so ranking a user rarely waits on embeddings (after a
// deploy, or when the model changes).
func (e *Embedder) Backfill(ctx context.Context, pool *pgxpool.Pool, maxTime time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, maxTime)
	defer cancel()
	ids, err := store.OpenJobsWithFacts(ctx, pool)
	if err != nil {
		return 0, err
	}
	n, err := e.EmbedJobs(ctx, pool, ids)
	if ctx.Err() != nil {
		return n, nil // out of time: the next run continues
	}
	return n, err
}
