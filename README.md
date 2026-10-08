# Wera

**Wera** (Swahili slang for a job or gig) is a self-hosted job radar. It collects postings from public job-board APIs (Greenhouse, Lever, Ashby), filters them with configurable rules, scores each one against your resume with an LLM on [Coral Bricks](https://www.coralbricks.ai), and serves the results through a REST API. Wera does not apply on your behalf. You review the matches and apply yourself.

## How it works

1. **Fetch** jobs from each company's public job board.
2. **Filter** with rules from `config/roles.yaml`: title, seniority, location, and explicit sponsorship refusals. No LLM cost.
3. **Score** the remaining jobs in parallel against your profile: fit score, seniority, years required, sponsorship status with the exact quote, and skills matched and missing. Your profile is sent as a cached prefix, so repeated calls cost very little.
4. **Serve** results, run history, and token and cost metrics over a REST API.

## Make it yours

Wera is driven entirely by configuration:

- `profile/profile.md`: your resume and preferences
- `config/companies.yaml`: the companies to watch
- `config/roles.yaml`: the role types, exclusions, and filters

Change these three files to point Wera at any role type, company list, or career stage. No code changes needed.

## Quick start

Requirements: Go 1.23+, Docker, and a Coral Bricks API key.

```bash
cp .env.example .env                               # add CORAL_API_KEY
cp profile/profile.example.md profile/profile.md   # add your resume and preferences
docker compose up -d postgres
make migrate                                       # create tables
go run ./cmd/wera fetch                            # fetch and filter, no LLM cost
go run ./cmd/wera score --limit 5                  # small scoring test
go run ./cmd/wera pipeline                         # one full run
go run ./cmd/wera serve                            # API at http://localhost:8080/api/today
```

## Run as a server

To run everything in Docker, with the frontend cloned next to this repo as `../wera-frontend`:

```bash
cp .env.example .env            # set CORAL_API_KEY and a random POSTGRES_PASSWORD
cp profile/profile.example.md profile/profile.md
docker compose up -d --build    # postgres, migrations, API, worker, dashboard
```

The dashboard is served at `http://<server-ip>:3000` (`WEB_PORT`). Migrations run automatically before the API and worker start. Postgres (`5433`) and the API (`8080`) bind to `127.0.0.1` only, so the dashboard is the only service other machines can reach. Docker-published ports bypass `ufw`, so to limit the dashboard to your LAN, add a rule to the `DOCKER-USER` chain:

```bash
iptables -I DOCKER-USER -p tcp -m conntrack --ctdir ORIGINAL --ctorigdstport 3000 ! -s 192.168.1.0/24 -j DROP
```

## Commands

| Command | Purpose |
|---|---|
| `wera pipeline` | Fetch, filter, and score once |
| `wera worker` | Run the pipeline on a schedule (`RUN_INTERVAL`) |
| `wera serve` | REST API, `/healthz`, and Prometheus `/metrics` |
| `wera discover "Name"` | Find a company's job board on Greenhouse, Lever, or Ashby |
| `wera companies validate` | Check `companies.yaml` |
| `wera refilter --all` | Reapply `roles.yaml` rules to existing jobs (no LLM cost) |
| `wera rescore --all` | Rescore after changing your profile |
| `wera deep --top N` | Longer review of top matches using Coral background mode |
| `wera bench` | Measure scoring throughput, cache hit rate, and cost per job |

## Extending

- **Add a company:** run `wera discover "Name"`, add it to `config/companies.yaml`, then run `wera companies validate` and `wera pipeline`.
- **Add a role type:** add a category and patterns to `config/roles.yaml`, then run `wera refilter --all`.
- **Add a job board provider:** implement the `Source` interface in `internal/sources` and register it.

## Project layout

```
cmd/wera            Single binary with subcommands
internal/sources    Greenhouse, Lever, and Ashby adapters
internal/normalize  HTML to text, content hashing
internal/filter     Rule engine driven by config/roles.yaml
internal/scoring    Coral Bricks client, prompt, cost accounting
internal/store      Postgres repository (pgx)
internal/pipeline   Fetch, filter, and score orchestration
internal/api        REST handlers (chi)
config/             Companies and roles (YAML)
profile/            Your resume and preferences (gitignored)
migrations/         Embedded SQL migrations (goose)
```

## Stack

Go, PostgreSQL, Coral Bricks (`glm-5.3-flash-fast`), Prometheus metrics, Docker Compose.

See `PLAN.md` for the full design.
