package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/config"
	"wera/internal/embed"
	"wera/internal/relevance"
	"wera/internal/scoring"
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
	// QueryPrefix goes before the profile text and DocPrefix before each
	// posting (see embed.Prefixes).
	QueryPrefix, DocPrefix string
	Log                    *slog.Logger
}

// NewEmbedder returns the configured embedder, or nil.
func NewEmbedder(env *config.Env, log *slog.Logger) *Embedder {
	c := embed.New(env.EmbedBaseURL, env.EmbedModel, env.EmbedAPIKey)
	if c == nil {
		return nil
	}
	q, d := embed.Prefixes(env.EmbedModel)
	if env.EmbedQueryPrefix != nil {
		q = *env.EmbedQueryPrefix
	}
	if env.EmbedDocPrefix != nil {
		d = *env.EmbedDocPrefix
	}
	return &Embedder{Client: c, Model: env.EmbedModel, QueryPrefix: q, DocPrefix: d, Log: log}
}

// profileKey and jobKey name stored vectors: the model, plus a mark of
// the prefix when there is one, so changing a prefix re-embeds instead of
// comparing vectors made two ways.
func (e *Embedder) profileKey() string { return keyed(e.Model, "q", e.QueryPrefix) }
func (e *Embedder) jobKey() string     { return keyed(e.Model, "d", e.DocPrefix) }

func keyed(model, kind, prefix string) string {
	if prefix == "" {
		return model
	}
	sum := sha256.Sum256([]byte(prefix))
	return model + "#" + kind + hex.EncodeToString(sum[:4])
}

// RankPending re-ranks the user's unscored matches against their profile
// without an AI call, stores the estimates (35..85) and returns them. The
// estimates order the AI scoring queue, decide which jobs get an AI score
// under the daily limit, and preview jobs on the dashboard.
//
// Jobs with shared facts are ranked by Blend (embedding similarity, word
// and skill matches on the facts, a penalty for asking too many years);
// the rest (new postings whose facts are not extracted yet) fall back to
// TF-IDF on the posting until they have facts.
func RankPending(ctx context.Context, pool *pgxpool.Pool, e *Embedder, prof *store.Profile) (map[int64]int, error) {
	docs, err := store.RankDocs(ctx, pool, prof.UserID)
	if err != nil {
		return nil, err
	}
	est := relevance.Rank(prof.Markdown, docs)
	if len(docs) > 0 {
		ids := make([]int64, len(docs))
		titles := make(map[int64]string, len(docs))
		for i, d := range docs {
			ids[i] = d.ID
			titles[d.ID] = d.Title
		}
		facts, err := store.FactsFor(ctx, pool, ids)
		if err != nil {
			return nil, err
		}
		var sims map[int64]float64
		if e != nil {
			if sims, err = e.Similarities(ctx, pool, prof, ids); err != nil {
				e.Log.Warn("embedding ranking unavailable; ranking by words and skills", "user_id", prof.UserID, "err", err)
			}
		}
		for id, v := range relevance.Scale(Blend(prof.Markdown, prof.Preferences, titles, facts, sims)) {
			est[id] = v
		}
	}
	return est, store.SetEstimates(ctx, pool, prof.UserID, est)
}

// Blend weights. Each signal is standardized over the user's candidates
// first, so the weights compare like with like. They were fit to 903 AI
// fit scores of six users (leave-one-user-out Spearman 0.60, against 0.46
// for embedding similarity alone); every held-out fit chose the same ones.
const (
	weightEmbedding = 1.0
	weightWords     = 1.0
	weightSkills    = 1.0
	// penaltyTooManyYears is subtracted when a posting asks for more
	// years than the user accepts: the AI scores those far lower.
	penaltyTooManyYears = 4.0
)

// FactsText is what a posting's facts read as for word matching and
// embedding: seniority, required skills and the digest.
func FactsText(f *scoring.Facts) string {
	return fmt.Sprintf("Seniority: %s. Skills: %s. %s", f.Seniority, strings.Join(f.SkillsRequired, ", "), f.Digest)
}

// Blend scores the jobs that have facts: embedding similarity (sims; a
// job missing from it counts as average), TF-IDF similarity of the facts
// to the profile's roles and skills, the share of required skills the
// profile names, minus a penalty for asking more years than the user
// accepts. Higher is better; the values only order jobs.
func Blend(markdown string, prefs config.Preferences, titles map[int64]string, facts map[int64]*scoring.Facts, sims map[int64]float64) map[int64]float64 {
	if len(facts) == 0 {
		return nil
	}
	profile := embed.ProfileText(markdown)
	have := relevance.TokenSet(profile)
	docs := make([]relevance.Doc, 0, len(facts))
	skills := make(map[int64]float64, len(facts))
	for id, f := range facts {
		docs = append(docs, relevance.Doc{ID: id, Title: titles[id], Body: FactsText(f)})
		skills[id] = relevance.SkillOverlap(have, f.SkillsRequired)
	}
	words := relevance.Standardize(relevance.Similarities(profile, docs))
	skills = relevance.Standardize(skills)
	emb := map[int64]float64{}
	if len(sims) > 0 {
		known := make(map[int64]float64, len(sims))
		for id := range facts {
			if v, ok := sims[id]; ok {
				known[id] = v
			}
		}
		emb = relevance.Standardize(known)
	}
	out := make(map[int64]float64, len(facts))
	for id, f := range facts {
		v := weightEmbedding*emb[id] + weightWords*words[id] + weightSkills*skills[id]
		if prefs.MaxYearsRequired > 0 && f.YearsRequired != nil && *f.YearsRequired > prefs.MaxYearsRequired {
			v -= penaltyTooManyYears
		}
		out[id] = v
	}
	return out
}

// Similarities returns the profile's similarity to each job that has
// facts, embedding the profile and any postings not embedded yet. On an
// error it still returns what it could compute.
func (e *Embedder) Similarities(ctx context.Context, pool *pgxpool.Pool, prof *store.Profile, jobIDs []int64) (map[int64]float64, error) {
	pv, err := store.ProfileEmbedding(ctx, pool, prof.UserID, prof.ProfileHash, e.profileKey())
	if err != nil {
		return nil, err
	}
	if pv == nil {
		vecs, err := e.Client.Embed(ctx, []string{e.QueryPrefix + embed.ProfileText(prof.Markdown)})
		if err != nil {
			return nil, fmt.Errorf("embed profile: %w", err)
		}
		pv = vecs[0]
		if err := store.SaveProfileEmbedding(ctx, pool, prof.UserID, prof.ProfileHash, e.profileKey(), pv); err != nil {
			return nil, err
		}
	}
	_, embedErr := e.EmbedJobs(ctx, pool, jobIDs)
	vecs, err := store.JobEmbeddings(ctx, pool, jobIDs, e.jobKey())
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
	todo, err := store.JobsToEmbed(ctx, pool, jobIDs, e.jobKey())
	if err != nil {
		return 0, err
	}
	done := 0
	for start := 0; start < len(todo); start += embedChunk {
		part := todo[start:min(start+embedChunk, len(todo))]
		texts := make([]string, len(part))
		for i, c := range part {
			texts[i] = e.DocPrefix + embed.JobText(c.Title, c.Facts.Seniority, c.Facts.SkillsRequired, c.Facts.Digest)
		}
		vecs, err := e.Client.Embed(ctx, texts)
		if err != nil {
			return done, fmt.Errorf("embed jobs: %w", err)
		}
		batch := make(map[int64][]float32, len(part))
		for i, c := range part {
			batch[c.ID] = vecs[i]
		}
		if err := store.SaveJobEmbeddings(ctx, pool, e.jobKey(), batch); err != nil {
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
