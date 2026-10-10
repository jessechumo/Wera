import sys
sys.path.insert(0, __import__('os').path.dirname(__file__))
from svgkit import Diagram, C, FAINT

d = Diagram(1240, 800, "Wera: system architecture",
            "Three containers and PostgreSQL on one host; public job boards and an LLM API outside. Arrows show who calls whom.")

# Trust boundaries
d.boundary(28, 108, 220, 300, "Users", C["slate"])
d.boundary(280, 108, 640, 640, "Docker host", C["blue"], note="only the web server is reachable from the network")
d.boundary(952, 108, 260, 640, "Internet", C["violet"])

# Users
d.card(48, 150, 180, 112, "browser", "Browser", ["React SPA", "session cookie (HttpOnly)", "light & dark themes"], C["slate"])
d.card(48, 282, 180, 96, "users", "Members", ["job seekers, mobile", "and desktop"], C["slate"])

# Host
d.card(310, 150, 280, 120, "shield", "wera-web", ["serves the built SPA", "proxies /api to wera-api", "CSP, HSTS, nosniff, no framing"], C["ember"], tag="nginx · :3000")
d.card(310, 330, 280, 168, "server", "wera-api", ["REST API (chi): auth, jobs, tracker", "rate limits per IP and account", "settings, profile, avatars, resumes",
                                                   "cover letters, community, interview", "moderation before publishing"], C["blue"], tag="Go · 127.0.0.1:8080")
d.card(620, 330, 280, 168, "clock", "wera-worker", ["scheduled runs (RUN_SCHEDULE)", "fetch → filter → rank → score", "pauses during Workday maintenance",
                                                      "catch-up run afterwards", "advisory lock: one run at a time"], C["teal"], tag="Go · same binary")
d.card(450, 560, 310, 136, "db", "PostgreSQL 16", ["jobs (shared) · user_jobs (per user)", "job_facts (shared) · analyses", "users, sessions, settings, files",
                                                    "posts, comments, questions"], C["green"], tag="127.0.0.1:5433 · goose migrations")

# Internet
d.card(972, 150, 220, 168, "briefcase", "Job boards", ["Greenhouse · Lever · Ashby", "Workday · SmartRecruiters", "Amazon · Eightfold", "public JSON APIs", "500+ companies"], C["amber"])
d.card(972, 400, 220, 150, "brain", "Inference API", ["Coral Bricks (OpenAI-style)", "facts, fit scores", "cover letters, drafts", "moderation verdicts"], C["violet"], tag="HTTPS only")

# Calls
d.arrow([(228, 196), (310, 196)], "HTTPS", C["slate"], label_at=(268, 186))
d.arrow([(450, 270), (450, 330)], "/api/*", C["ember"], label_at=(478, 304))
d.arrow([(450, 498), (520, 560)], "SQL (pgx)", C["green"], label_at=(452, 538))
d.arrow([(760, 498), (690, 560)], "SQL", C["green"], label_at=(752, 538))
d.arrow([(900, 360), (936, 360), (936, 234), (972, 234)], "ETag / 304\nincremental\nthrottled", C["amber"], label_at=(930, 290))
d.arrow([(900, 450), (972, 450)], "facts once\nper posting", C["violet"], label_at=(936, 470))
d.arrow([(380, 498), (380, 716), (1082, 716), (1082, 550)], "on a user's request: score now, cover letter, profile draft, moderation", C["violet"], dashed=True, label_at=(700, 712))

# Legend
y = 782
x = 290
for text, col in [("solid: scheduled", C["slate"]), ("dashed: on a user's request", C["slate"])]:
    x += d.chip(x, y - 14, text, col) + 10
d.text(1212, y, "Data stays on the host; secrets in .env (600)", 11, FAINT, anchor="end")

open(sys.argv[1], "w").write(d.svg())
