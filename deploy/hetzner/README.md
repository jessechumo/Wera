# Deploying Wera to a server (Hetzner)

Everything runs in Docker from the images each release publishes to GitHub
Container Registry (`ghcr.io/jessechumo/wera`, `wera-frontend`,
`wera-admin`). The server needs only this folder.

```
internet ──> Caddy :80/:443 (free Let's Encrypt HTTPS, renewed automatically)
               ├── /api/*, /healthz ──> wera-api ──┐
               └── everything else  ──> wera-web   ├──> postgres (no public port)
             wera-worker (scheduled fetch + scoring)┘
             wera-admin on 127.0.0.1:3001 (SSH tunnel only)
```

Any Hetzner Cloud server with 4 GB of RAM or more works (x86 or Arm: the
images are built for both). Ubuntu 24.04.

## 1. Domain

At your registrar (or Cloudflare, if it manages the domain's DNS), add:

| Type | Name | Value |
|---|---|---|
| A | `@` | the server's IPv4 |
| A | `www` | the server's IPv4 |
| AAAA | `@` and `www` | the server's IPv6 (optional) |

With Cloudflare, set these records to **DNS only** (grey cloud) for the first
start, so Caddy can get its certificate; switching to proxied later is fine.
You do not need to buy an SSL certificate: Caddy gets one for free.

## 2. Server

Create the server in Hetzner Cloud with your SSH key, then as root:

```bash
curl -fsSL https://raw.githubusercontent.com/jessechumo/Wera/main/deploy/hetzner/setup-server.sh | bash
```

That installs Docker, enables the firewall (SSH, 80, 443 only), automatic
security updates and swap, creates a `wera` user with your SSH key, turns off
root and password logins, puts these files in `/home/wera/wera`, and
schedules a nightly database backup at 03:30.

## 3. Configure and start

```bash
ssh wera@<server-ip>
cd ~/wera
./deploy.sh init      # creates .env with a random database password
nano .env             # WERA_DOMAIN, ACME_EMAIL, AI_PROVIDER, AI_API_KEY (and email, see below)
./deploy.sh up
./deploy.sh status    # containers, then https://<domain>/healthz
```

Open `https://<domain>`: the first request may take a few seconds while
Caddy gets the certificate.

## 4. Move the existing data (optional)

On the machine that runs Wera now:

```bash
docker exec wera-postgres pg_dump -U wera -d wera -Fc > wera.dump
scp wera.dump wera@<server-ip>:~/wera/
```

On the server: `./deploy.sh restore wera.dump`. It replaces the server's
database, then starts everything; migrations bring the schema up to date.
Delete `wera.dump` afterwards, and stop the old machine's worker so jobs are
not fetched twice.

## Day to day

| Task | Command |
|---|---|
| Upgrade to the newest release | `./deploy.sh update` (backs up first) |
| Logs | `./deploy.sh logs` or `./deploy.sh logs wera-worker` |
| Back up now | `./deploy.sh backup` (dumps in `./backups`, the newest `BACKUP_KEEP` kept) |
| Wera CLI | `./deploy.sh wera users list`, `./deploy.sh wera mail test --to you@example.com` |
| Change settings | edit `.env`, then `./deploy.sh up` |

Backups stay on the same disk. Copy them off now and then
(`scp wera@<server-ip>:~/wera/backups/*.dump .`), or turn on Hetzner's server
backups.

**Admin console.** It is not reachable from the internet. From your laptop:

```bash
ssh -N -L 3001:127.0.0.1:3001 wera@<server-ip>
```

then open http://localhost:3001 and sign in as usual.

## Email

Email (verification, password reset, welcome, digests) is built in and stays
off until it is configured. You need:

1. A sending provider with SMTP, for example Resend, Amazon SES, Postmark or
   Brevo. Add the domain there.
2. The DNS records the provider shows (SPF and DKIM), plus DMARC:
   `TXT _dmarc  "v=DMARC1; p=quarantine; rua=mailto:dmarc@<domain>"`.
3. In `.env`: `MAIL_PROVIDER=smtp`, `MAIL_FROM`, `MAIL_REPLY_TO` and the
   `SMTP_*` settings, then `./deploy.sh up`.
4. Check: `./deploy.sh wera mail test --to you@example.com`.

Hetzner blocks outgoing ports 25 and 465 on new servers, so use port 587
with `SMTP_SECURITY=starttls`. To receive mail at `support@<domain>`, use
forwarding (Cloudflare Email Routing is free) or a mailbox provider.
