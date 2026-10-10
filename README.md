# Wera

**Wera** (Swahili slang for a job or gig) is a self-hosted job radar. It collects postings from public job-board feeds (Greenhouse, Lever, Ashby, Workday, SmartRecruiters, Eightfold, and amazon.jobs) for 570+ companies across 28 industries, filters them with configurable rules, scores each one against your resume with an LLM on [Coral Bricks](https://www.coralbricks.ai), and serves the results through a REST API. Wera does not apply on your behalf. You review the matches and apply yourself.

## How it works

1. **Fetch** jobs once from each company's public job board, for everyone.
2. **Filter** per user with the rules in `config/roles.yaml` and that user's preferences: role families, seniority levels, US-only, and whether they need visa sponsorship. No LLM cost.
3. **Score** each user's remaining jobs in parallel against their profile: fit score, seniority, years required, sponsorship status with the exact quote, and skills matched and missing. The profile is sent as a cached prefix, so repeated calls cost very little, and users with identical profile text share scores.
4. **Serve** each user their own matches, tracker, run history, and metrics over a REST API.

## Make it yours

Wera is driven by configuration and per-user profiles:

- `config/companies.yaml`: the companies to watch, each tagged with an industry from `config/industries.yaml`
- `config/roles.yaml`: the catalog of role families and seniority levels users pick from, plus location and sponsorship rules
- Each user's profile (stored in the database): the text the LLM scores against and their filter preferences

New users build their profile in the dashboard: they upload a resume PDF (or paste the text), answer a short questionnaire (experience, work authorization, locations, work modes, industries, target roles) and pick role families and seniority levels. The LLM drafts the profile from the resume and answers, the user reviews and edits it, and saving it starts matching right away. Only the resume's extracted text is kept, not the file.

## Quick start

Requirements: Go 1.23+, Docker, and a Coral Bricks API key.

```bash
cp .env.example .env                               # add CORAL_API_KEY
docker compose up -d postgres
make migrate                                       # create tables
go run ./cmd/wera serve                            # API on http://localhost:8080
```

Then run the dashboard (`../wera-frontend`, `npm run dev`), sign up, and set up your profile; saving it matches and scores your jobs. `go run ./cmd/wera pipeline` runs one full fetch, filter, and score for every user.

To use an existing `profile/profile.md` instead of the setup flow, create an account and import it:

```bash
go run ./cmd/wera users create --email you@example.com --admin
go run ./cmd/wera users import-profile --email you@example.com --file profile/profile.md
```

The profile still needs role families and levels, which you pick on the dashboard's Profile page.

## Run as a server

To run everything in Docker, with the frontend cloned next to this repo as `../wera-frontend`:

```bash
cp .env.example .env            # set CORAL_API_KEY and a random POSTGRES_PASSWORD
docker compose up -d --build    # postgres, migrations, API, worker, dashboard
```

The dashboard is served at `http://<server-ip>:3000` (`WEB_PORT`). Migrations run automatically before the API and worker start. Postgres (`5433`) and the API (`8080`) bind to `127.0.0.1` only, so the dashboard is the only service other machines can reach. Docker-published ports bypass `ufw`, so to limit the dashboard to your LAN, add a rule to the `DOCKER-USER` chain:

```bash
iptables -I DOCKER-USER -p tcp -m conntrack --ctdir ORIGINAL --ctorigdstport 3000 ! -s 192.168.1.0/24 -j DROP
```

### Public access

To let people outside your network use Wera, host the dashboard on Vercel (see the frontend README) and give the API a public HTTPS address without opening router ports, for example with a [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) to `http://localhost:8080`. Then set `COOKIE_SECURE=true`, `TRUST_PROXY=true`, and `PUBLIC_ORIGINS=https://<your-app>.vercel.app` in `.env` and restart the API.

### Upgrading from single-user Wera

Migration 0004 moves existing jobs, scores, and tracker statuses to an admin account named `owner@wera.local` with no password. Claim it:

```bash
docker exec wera-api /app/wera users update --email owner@wera.local --new-email you@example.com --name You
docker exec wera-api /app/wera users passwd --email you@example.com        # prints a password
docker exec wera-api /app/wera users import-profile --email you@example.com --file /app/profile/profile.md
```

Importing the same `profile.md` keeps every existing score: scores are keyed by the profile text, so nothing is rescored.

### Accounts

Every API route except signup and login needs a session. People sign up in the dashboard (turn this off with `SIGNUP_ENABLED=false`), or you create accounts from the shell; `create` and `passwd` print a generated password:

```bash
docker exec wera-api /app/wera users create --email you@example.com --name You --admin
```

Admins can trigger runs and see overall usage and each user's spend. Everyone shares one Coral Bricks key, so spending is capped three ways: per user per run (`MAX_COST_PER_RUN_USD`), per user per month (`USER_MONTHLY_BUDGET_USD`, default $10; change one user's with `wera users budget`), and for all users together per month (`MAX_MONTHLY_COST_USD`). Jobs over a limit stay pending until the next month or a higher budget. Sessions are HTTP-only, `SameSite=Lax` cookies that last 30 days; only a hash of each token is stored. Signup and login are rate-limited per IP.

## Commands

| Command | Purpose |
|---|---|
| `wera pipeline` | Fetch, filter, and score once |
| `wera worker` | Run the pipeline at the `RUN_SCHEDULE` times in `RUN_TIMEZONE` (or every `RUN_INTERVAL` when no schedule is set) |
| `wera serve` | REST API, `/healthz`, and Prometheus `/metrics` |
| `wera discover "Name"` | Find a company's job board on Greenhouse, Lever, or Ashby |
| `wera companies validate` | Check `companies.yaml` |
| `wera refilter --all [--user E]` | Reapply `roles.yaml` rules to existing jobs (no LLM cost) |
| `wera rescore --all [--user E]` | Rescore after changing profiles |
| `wera deep --top N [--user E]` | Longer review of a user's top matches using Coral background mode |
| `wera bench [--user E]` | Measure scoring throughput, cache hit rate, and cost per job |
| `wera users list\|create\|passwd\|admin\|update\|budget\|import-profile` | Manage accounts and profiles from the server shell |

## Extending

- **Add a company:** run `wera discover "Name"`, add it to `config/companies.yaml`, then run `wera companies validate` and `wera pipeline`.
- **Add a role type:** add a family and patterns to `config/roles.yaml`; users can then pick it.
- **Add an industry:** add it to `config/industries.yaml` and tag companies with it.
- **Add a company on Workday:** its token is `tenant.wdN/site` from the career site URL, e.g. `https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite` is `nvidia.wd5/NVIDIAExternalCareerSite`. SmartRecruiters takes the company identifier, Eightfold `host/domain`.
- **Add a job board provider:** implement the `Source` interface in `internal/sources` (or `DetailSource` when the list has no descriptions, so only new postings are fetched in detail) and register it.

## Project layout

```
cmd/wera            Single binary with subcommands
internal/sources    Greenhouse, Lever, Ashby, Workday, SmartRecruiters, Eightfold, Amazon adapters
internal/normalize  HTML to text, content hashing
internal/filter     Rule engine driven by config/roles.yaml
internal/scoring    Coral Bricks client, prompt, cost accounting
internal/store      Postgres repository (pgx)
internal/pipeline   Fetch, filter, and score orchestration
internal/api        REST handlers (chi)
config/             Companies, industries, and the role catalog (YAML)
internal/auth       Password hashing, session tokens, rate limiting
internal/profile    Resume text extraction and the profile-drafting prompt
profile/            Optional profile.md to import (gitignored)
migrations/         Embedded SQL migrations (goose)
```

## Stack

Go, PostgreSQL, Coral Bricks (`glm-5.3-flash-fast`), Prometheus metrics, Docker Compose.

See `PLAN.md` for the full design.
