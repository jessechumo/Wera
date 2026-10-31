#!/usr/bin/env bash
# Brings the public site back after the network drops. Cron runs this every
# minute on the home server (install-tunnel-watch.sh sets that up).
#
# The dashboard is on Vercel, which sends /api through a Cloudflare quick
# tunnel to this machine. A quick tunnel rides out short drops. After about
# 12 minutes offline Cloudflare forgets it: cloudflared stays up, retrying
# forever with "control stream encountered a failure", and the site stays
# down with the network back. Only a new tunnel works, and it has a new
# address, which has to reach vercel.json. This does both.
#
# It does nothing while the site answers, while this machine is offline (a
# new tunnel needs the internet, and the old one may yet recover), while the
# API itself is down (a new tunnel would not help), or when the tunnel was
# stopped on purpose. `touch ~/.local/state/wera-tunnel/paused` turns it off.
set -uo pipefail
export PATH=/usr/local/bin:/usr/bin:/bin

SITE=${WERA_SITE:-https://wera-frontend-five.vercel.app}
REPO=${WERA_FRONTEND_REPO:-git@github.com:jessechumo/Wera-Frontend.git}
TUNNEL=${WERA_TUNNEL_CONTAINER:-wera-tunnel}
API=${WERA_API_CONTAINER:-wera-api-public}
STATE=${WERA_WATCH_STATE:-$HOME/.local/state/wera-tunnel}
KEY=${WERA_DEPLOY_KEY:-$HOME/.config/wera/tunnel-deploy-key}
ONLINE_URL=${WERA_ONLINE_URL:-https://www.cloudflare.com/cdn-cgi/trace}
# Failed checks before a tunnel with no sign of being stuck is replaced: the
# longest drop one recovered from on its own took 5m39s.
PATIENCE=${WERA_WATCH_PATIENCE:-7}
# Seconds between new tunnels (Cloudflare limits how fast they can be made).
RESTART_GAP=${WERA_WATCH_RESTART_GAP:-300}
# How long a new tunnel gets to answer before the next run looks again.
SETTLE=${WERA_WATCH_SETTLE:-40}

log() {
  printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >>"$STATE/watch.log"
  if [ "$(wc -l <"$STATE/watch.log")" -gt 2000 ]; then
    tail -n 1000 "$STATE/watch.log" >"$STATE/watch.log.new" && mv "$STATE/watch.log.new" "$STATE/watch.log"
  fi
}

# Says something once, until mark clear forgets it (a marker file per state,
# so a long outage logs one line and not one a minute).
mark() {
  [ -e "$STATE/$1" ] && return 0
  : >"$STATE/$1"
  log "$2"
}

clear_marks() {
  rm -f "$STATE/offline" "$STATE/api-down" "$STATE/waiting-vercel" "$STATE/failed-checks"
}

# Whether a Wera address is up, from the outside.
answers() {
  curl -fsS -m 10 "$1/healthz" 2>/dev/null | grep -q '"status":"ok"'
}

online() {
  curl -fsS -m 8 -o /dev/null "$ONLINE_URL" 2>/dev/null
}

container() {
  docker inspect -f "$2" "$1" 2>/dev/null
}

# The address of the running quick tunnel, from cloudflared's metrics port.
tunnel_host() {
  local ip
  ip=$(container "$TUNNEL" '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}') || return 1
  curl -fsS -m 3 "http://$ip:20241/quicktunnel" 2>/dev/null | sed -n 's/.*"hostname":"\([^"]*\)".*/\1/p'
}

# cloudflared's sign that Cloudflare has forgotten the tunnel: of 18 drops
# logged, it appeared in all 6 that never recovered and in none that did.
stuck() {
  [ "$(docker logs --since 3m "$TUNNEL" 2>&1 | grep -c 'control stream encountered a failure')" -ge 3 ]
}

new_tunnel() {
  local now last
  now=$(date +%s)
  last=$(cat "$STATE/restarted" 2>/dev/null || echo 0)
  [ $((now - last)) -ge "$RESTART_GAP" ] || return 1
  echo "$now" >"$STATE/restarted"
  log "tunnel is down with the internet up: starting a new one"
  docker restart "$TUNNEL" >/dev/null
}

# Puts the address in vercel.json on main (which Vercel deploys) and on dev
# (so the next dev to main merge does not bring the old one back). A file
# that already has it stays as it is, so a slow deploy is not pushed twice.
publish() {
  local host=$1 dir=$STATE/frontend branch pushed=
  export GIT_SSH_COMMAND="ssh -i $KEY -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new"
  if [ ! -d "$dir/.git" ]; then
    git clone -q --depth 1 --no-single-branch "$REPO" "$dir" 2>>"$STATE/watch.log" || return 1
  fi
  for branch in main dev; do
    git -C "$dir" fetch -q --depth 1 origin "$branch" 2>>"$STATE/watch.log" || return 1
    git -C "$dir" checkout -q -f -B "$branch" FETCH_HEAD 2>>"$STATE/watch.log" || return 1
    sed -i -E "s#https://[a-z0-9-]+\.trycloudflare\.com#https://$host#g" "$dir/vercel.json"
    git -C "$dir" diff --quiet && continue
    git -C "$dir" -c user.name="${WERA_GIT_NAME:-jesse}" -c user.email="${WERA_GIT_EMAIL:-jessekip21@gmail.com}" \
      commit -qam "chore(deploy): point Vercel rewrites at the new tunnel URL" 2>>"$STATE/watch.log" || return 1
    git -C "$dir" push -q origin "$branch" 2>>"$STATE/watch.log" || return 1
    pushed=1
  done
  if [ -n "$pushed" ]; then
    log "pointed Vercel at https://$host"
  else
    mark waiting-vercel "vercel.json already has https://$host: waiting for Vercel"
  fi
}

check() {
  if answers "$SITE"; then
    [ -e "$STATE/down" ] && log "site is back after $(($(date +%s) - $(cat "$STATE/down")))s"
    rm -f "$STATE/down"
    clear_marks
    return 0
  fi
  [ -e "$STATE/down" ] || { date +%s >"$STATE/down"; log "site is not answering"; }

  if ! online; then
    mark offline "this machine is offline: waiting for the network"
    return 0
  fi
  rm -f "$STATE/offline"

  # Stopped on purpose: Docker itself restarts a tunnel that crashed.
  [ "$(container "$TUNNEL" '{{.State.Status}}')" = running ] || return 0
  if [ "$(container "$API" '{{.State.Health.Status}}')" != healthy ]; then
    mark api-down "$API is not healthy: leaving the tunnel alone"
    return 0
  fi
  rm -f "$STATE/api-down"

  local host fails=0
  host=$(tunnel_host)
  if [ -n "$host" ] && answers "https://$host"; then
    rm -f "$STATE/failed-checks"
    publish "$host" || log "could not update vercel.json (trying again next run)"
    return 0
  fi

  [ -e "$STATE/failed-checks" ] && fails=$(cat "$STATE/failed-checks")
  fails=$((fails + 1))
  echo "$fails" >"$STATE/failed-checks"
  if stuck || [ "$fails" -ge "$PATIENCE" ]; then
    new_tunnel || return 0
    rm -f "$STATE/failed-checks"
    # Publish in this run when the new tunnel is quick, a minute sooner.
    local waited=0
    while [ "$waited" -lt "$SETTLE" ]; do
      sleep 5
      waited=$((waited + 5))
      host=$(tunnel_host)
      if [ -n "$host" ] && answers "https://$host"; then
        publish "$host" || log "could not update vercel.json (trying again next run)"
        return 0
      fi
    done
  fi
}

main() {
  mkdir -p "$STATE"
  [ -e "$STATE/paused" ] && return 0
  exec 9>"$STATE/lock"
  flock -n 9 || return 0
  check
}

main
