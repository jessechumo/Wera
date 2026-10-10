import sys
sys.path.insert(0, __import__('os').path.dirname(__file__))
from svgkit import Diagram, C, FAINT, MUTED

d = Diagram(1240, 600, "Wera: matching pipeline",
            "From job boards to a ranked list. Most work is shared across users or done locally; the LLM sees each posting once.")

W, H = 270, 150
row1 = [36, 336, 636, 936]
row2 = [96, 396, 696]
y1, y2 = 104, 330

def stage(x, y, n, icon, title, lines, color, cost, cost_color):
    d.card(x, y, W, H, icon, f"{n}. {title}", lines, color)
    d.chip(x + 16, y + H - 32, cost, cost_color)

shared, local, llm = C["green"], C["teal"], C["violet"]
stage(row1[0], y1, 1, "briefcase", "Fetch boards", ["ETag sent: unchanged boards answer 304", "Workday: new postings only, full", "sweep daily; shared polite throttle"], C["amber"], "unchanged board ≈ 0 bytes", C["amber"])
stage(row1[1], y1, 2, "db", "Store changes", ["content hash per posting: only new", "or changed rows are written", "bulk COPY upsert"], shared, "shared by all users", shared)
stage(row1[2], y1, 3, "filter", "Rule filter", ["per user: role families, seniority,", "US-only, sponsorship refusals", "(regex on keyword windows)"], local, "free · 40k jobs ≈ 4 s", local)
stage(row1[3], y1, 4, "chart", "Local ranking", ["TF-IDF match to the profile gives", "an estimated score, so a new user", "sees ranked jobs in seconds"], local, "free · no LLM", local)
stage(row2[0], y2, 5, "sparkle", "Shared facts", ["one LLM call per posting version:", "seniority, years, sponsorship", "(quote must appear verbatim)"], llm, "1 call per posting, all users", llm)
stage(row2[1], y2, 6, "shield", "Fact exclusions", ["too many years, no sponsorship,", "outside the US, too senior:", "excluded before any fit call"], local, "free · ~half of jobs drop", local)
stage(row2[2], y2, 7, "brain", "Fit score", ["compact job card + profile, cached", "prompt prefix; best estimates first,", "or instantly when a job is opened"], llm, "≈ $0.0004 per job", llm)

# flow arrows
for a, b in zip(row1, row1[1:]):
    d.arrow([(a + W, y1 + 70), (b, y1 + 70)], color=C["slate"])
d.arrow([(row1[3] + W / 2, y1 + H), (row1[3] + W / 2, y1 + H + 38), (row2[0] + W / 2, y1 + H + 38), (row2[0] + W / 2, y2)], color=C["slate"])
for a, b in zip(row2, row2[1:]):
    d.arrow([(a + W, y2 + 70), (b, y2 + 70)], color=C["slate"])

# outcome
d.card(row2[2] + W + 40, y2 + 18, 168, 114, "layers", "Your lists", ["Today, Jobs,", "Industries, Tracker"], C["ember"])
d.arrow([(row2[2] + W, y2 + 70), (row2[2] + W + 40, y2 + 70)], color=C["slate"])

# measured outcomes
yb = 528
d.text(36, yb, "Measured on the dev data:", 12, MUTED, 650)
x = 200
for t, col in [("new user ready in ~5 s (was 5 min)", C["ember"]), ("fit call $0.00038 vs $0.00075", llm),
               ("22 of 46 jobs excluded without a fit call", local), ("refilter 43,732 jobs: 9.3 s → 4.1 s", shared)]:
    x += d.chip(x, yb - 14, t, col) + 10
d.text(36, 566, "Legend:", 11, FAINT)
x = 90
for t, col in [("shared across users", shared), ("local, free", local), ("LLM", llm), ("job boards", C["amber"])]:
    x += d.chip(x, 552, t, col) + 8

open(sys.argv[1], "w").write(d.svg())
