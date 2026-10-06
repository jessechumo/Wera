# Wera

**Wera** (Swahili slang for job/gig) collects entry-level infrastructure
jobs from public job-board APIs (Greenhouse, Lever, Ashby), filters them
with rules, scores each one against a resume with an LLM on Coral Bricks,
and serves the results over a REST API for a dashboard. It never applies on
your behalf — you review and apply yourself.

See `PLAN.md` for the full design.

## Quick start (local, Ubuntu)

```bash
cp .env.example .env                  # add CORAL_API_KEY
cp profile/profile.example.md profile/profile.md   # paste resume + preferences
docker compose up -d postgres
make migrate                          # create tables
go run ./cmd/wera fetch               # fetch + rule filter, no LLM spend
go run ./cmd/wera score --limit 5     # tiny paid test
go run ./cmd/wera pipeline            # one full run
go run ./cmd/wera serve               # http://localhost:8080/api/today
```

## Layout

- `cmd/wera` — single binary, subcommand dispatch
- `internal/sources` — Greenhouse/Lever/Ashby adapters
- `internal/normalize` — HTML to text, content hashing
- `internal/filter` — rule engine driven by `config/roles.yaml`
- `internal/scoring` — Coral Bricks client, prompt, cost accounting
- `internal/store` — pgx repository functions
- `internal/pipeline` — orchestration (fetch → filter → score)
- `internal/api` — chi REST handlers
- `config/` — companies and roles (YAML, no code changes to extend)
- `profile/` — resume + preferences (gitignored)
- `migrations/` — embedded goose SQL

## Adding things later (no code changes)

- **New company:** `wera discover "Name"`, add a line to
  `config/companies.yaml`, `wera companies validate`, `wera pipeline`.
- **New role type:** add a category and patterns to `config/roles.yaml`,
  then `wera refilter --all` (free, no LLM).
