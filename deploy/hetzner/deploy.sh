#!/usr/bin/env bash
# Day-to-day commands for Wera on the server. Run from this folder.
#
#   ./deploy.sh init              create .env with a random database password
#   ./deploy.sh up                start (or apply .env changes)
#   ./deploy.sh update            pull the newest images and restart
#   ./deploy.sh status            containers and health
#   ./deploy.sh logs [service]    follow logs (all services by default)
#   ./deploy.sh backup            dump the database to ./backups
#   ./deploy.sh restore <file>    replace the database with a dump (asks first)
#   ./deploy.sh wera <args>       run the wera CLI, e.g. wera users list
set -euo pipefail

cd "$(dirname "$0")"
compose() { docker compose --project-name wera "$@"; }

need_env() {
	if [[ ! -f .env ]]; then
		echo "No .env yet: run ./deploy.sh init and fill it in." >&2
		exit 1
	fi
}

env_value() { grep -E "^$1=" .env | tail -1 | cut -d= -f2-; }

backup() {
	need_env
	mkdir -p backups
	local file
	file="backups/wera-$(date +%Y%m%d-%H%M%S).dump"
	compose exec -T postgres pg_dump -U wera -d wera -Fc >"$file.part"
	mv "$file.part" "$file"
	echo "Saved $file ($(du -h "$file" | cut -f1))"
	local keep
	keep=$(env_value BACKUP_KEEP || true)
	keep=${keep:-14}
	# shellcheck disable=SC2012 # our own wera-<timestamp>.dump names
	ls -1t backups/wera-*.dump | tail -n +"$((keep + 1))" | xargs -r rm --
}

case "${1:-}" in
init)
	if [[ -f .env ]]; then
		echo ".env already exists; edit it directly." >&2
		exit 1
	fi
	cp .env.example .env
	chmod 600 .env
	sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 24)/" .env
	echo "Created .env. Set WERA_DOMAIN, ACME_EMAIL, AI_PROVIDER and AI_API_KEY, then ./deploy.sh up"
	;;
up)
	need_env
	compose pull
	compose up -d --remove-orphans
	compose ps
	;;
update)
	need_env
	backup
	compose pull
	compose up -d --remove-orphans
	docker image prune -f >/dev/null
	compose ps
	;;
status)
	compose ps
	domain=$(env_value WERA_DOMAIN || true)
	[[ -n "$domain" ]] && curl -fsS --max-time 10 "https://$domain/healthz" && echo
	;;
logs)
	shift
	compose logs -f --tail=100 "$@"
	;;
backup)
	backup
	;;
restore)
	need_env
	file=${2:?usage: ./deploy.sh restore <file.dump>}
	[[ -f "$file" ]] || { echo "No such file: $file" >&2; exit 1; }
	read -r -p "This replaces the whole Wera database with $file. Type 'restore' to go on: " answer
	[[ "$answer" == "restore" ]] || { echo "Cancelled."; exit 1; }
	compose up -d postgres
	until compose exec -T postgres pg_isready -U wera -d wera >/dev/null 2>&1; do sleep 1; done
	compose stop wera-api wera-worker wera-web wera-admin caddy 2>/dev/null || true
	compose exec -T postgres dropdb -U wera --if-exists --force wera
	compose exec -T postgres createdb -U wera wera
	compose exec -T postgres pg_restore -U wera -d wera --no-owner --no-privileges <"$file"
	echo "Restored. Starting everything (migrations run first)."
	compose up -d --remove-orphans
	;;
wera)
	shift
	compose exec wera-api /app/wera "$@"
	;;
*)
	sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
