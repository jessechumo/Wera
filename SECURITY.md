# Security

## Reporting a vulnerability

Please report security issues privately to **jessechumo@gmail.com** rather than
opening a public issue. Include the steps to reproduce and the impact you
expect. You will get a reply within a few days; fixes ship as patch releases.

## How Wera protects users

This summarizes the controls in place, reviewed end to end (API, worker,
frontend, deployment) in October 2026.

### Accounts and sessions

- Passwords are hashed with bcrypt; at least 10 characters are required.
- Sessions are random 256-bit tokens, stored only as hashes, sent in an
  `HttpOnly`, `SameSite=Lax` cookie (`Secure` when served over HTTPS).
  Changing the password signs out every other session; deleting the
  account requires the password and removes all of the user's data.
- Logins are rate limited per client IP **and** per account, so guesses
  spread across many addresses still stop. Behind the proxy, the client IP
  comes from the header the proxy overwrites (`X-Real-IP`), never from
  client-controlled ones.

### Requests

- State-changing requests must come from the app's own origin (`Origin`
  check, plus Fetch Metadata: `Sec-Fetch-Site: cross-site` is refused).
- Every JSON body is capped at 1 MiB; uploads have exact limits (resume
  PDF 5 MiB, profile picture 40 megapixels), and images are decoded,
  cropped and re-encoded as JPEG, so no uploaded bytes are served back.
- The HTTP server has read, write, idle and header timeouts.
- All SQL is parameterized; the few dynamic fragments (sort order,
  filters) are chosen from fixed lists.
- Every user-owned query is scoped to the signed-in user; admin-only
  routes are checked server side.

### Responses

- The app sends a strict Content-Security-Policy (first-party scripts
  only, no inline scripts, no framing, network calls only to its own
  API), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, a
  strict referrer policy, a restrictive Permissions-Policy, COOP and HSTS.
- API responses are `no-store` and carry a sandboxing CSP.
- The frontend never interprets HTML from the API or from users: React
  escapes all text, community posts use a small markdown subset rendered
  as elements, and only `https` links become anchors
  (`rel="nofollow noopener noreferrer ugc"`).
- Fonts and scripts are self-hosted: no third-party requests.

### AI features (prompt injection)

Job postings, resumes and community posts are untrusted text that reaches
an LLM. Every prompt:

- wraps that text in data tags (`<posting>`, `<profile>`, `<resume>`,
  `<answers>`, `<content>`) and tells the model it is data, never
  instructions;
- neutralizes those tags inside the text in any case or spacing, so it
  cannot close its wrapper (`scoring.Fence`);
- accepts only replies that match a fixed schema. Scores, verdicts and
  categories are validated against allowed values, and a sponsorship
  quote must appear verbatim in the posting.

Community moderation fails closed: if no valid verdict comes back,
nothing is published, and a flagged category always rejects, even if the
reply claims the post is allowed.

### Transport and secrets

- All outbound calls (job boards, the inference API) use HTTPS with
  certificate verification. The inference base URL is refused at startup
  unless it is HTTPS (or http to localhost for tests).
- Secrets live only in `.env` (mode 600, git-ignored, never committed)
  and are never logged.
- PostgreSQL and the API listen on 127.0.0.1 only; only the web server
  is reachable on the network.
- On a LAN, the site is plain HTTP. For internet hosting, put it behind
  TLS (Cloudflare Tunnel or a TLS proxy) and set `COOKIE_SECURE=true`;
  HSTS then prevents downgrade (MITM) attacks.

### Dependencies

`govulncheck` and `npm audit` run in CI and report no known
vulnerabilities in reachable code. Dependabot opens update PRs.

### Being a good citizen to job boards

Wera reads public career-site APIs politely: conditional requests
(ETags, 304s), incremental listing with a daily full sweep, a shared
throttle (few concurrent requests, spaced out), per-run caps, an
identifying User-Agent, and a pause during Workday's weekly maintenance
window.
