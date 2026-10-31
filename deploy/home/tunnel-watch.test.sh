#!/usr/bin/env bash
# Tests tunnel-watch.sh with stand-ins for docker and curl and a real git
# repository in place of the frontend's. Each case sets what the world looks
# like and checks what the watch did about it.
set -uo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT
FAILED=0

# What cloudflared logs once Cloudflare has forgotten its tunnel.
STUCK='ERR Register tunnel error from server side error="Unauthorized: Tunnel not found"'
OLD_STUCK='ERR Serve tunnel error error="control stream encountered a failure while serving"'
OLD=old-address-here.trycloudflare.com
NEW=new-address-here.trycloudflare.com

mkdir -p "$ROOT/bin"
# The world, as files in $WORLD: site-up, online, tunnel-up, stuck, host,
# api-health, tunnel-state. docker restart gives the tunnel a new address.
cat >"$ROOT/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  inspect)
    case "$3" in
      *IPAddress*) echo 10.0.0.9 ;;
      *State.Status*) cat "$WORLD/tunnel-state" ;;
      *Health*) cat "$WORLD/api-health" ;;
    esac ;;
  logs) [ -e "$WORLD/stuck" ] && printf '%s\n' "$(cat "$WORLD/stuck")" "$(cat "$WORLD/stuck")" "$(cat "$WORLD/stuck")" ;;
  restart)
    echo restart >>"$WORLD/restarts"
    echo "$WERA_TEST_NEW" >"$WORLD/host"
    rm -f "$WORLD/stuck"
    [ -e "$WORLD/restart-heals" ] && : >"$WORLD/tunnel-up" ;;
esac
exit 0
EOF
cat >"$ROOT/bin/curl" <<'EOF'
#!/usr/bin/env bash
url=${*: -1}
case "$url" in
  https://site.test/healthz) [ -e "$WORLD/site-up" ] && echo '{"status":"ok"}' || exit 22 ;;
  https://online.test) [ -e "$WORLD/online" ] || exit 6 ;;
  http://10.0.0.9:20241/quicktunnel) echo "{\"hostname\":\"$(cat "$WORLD/host")\"}" ;;
  https://*.trycloudflare.com/healthz) [ -e "$WORLD/tunnel-up" ] && echo '{"status":"ok"}' || exit 22 ;;
  *) echo "unexpected curl: $url" >&2; exit 99 ;;
esac
EOF
chmod +x "$ROOT/bin/docker" "$ROOT/bin/curl"

# A fresh world: the site up, the internet up, the tunnel healthy.
fresh() {
  export WORLD=$ROOT/world
  rm -rf "$WORLD" "$ROOT/state" "$ROOT/origin.git" "$ROOT/seed"
  mkdir -p "$WORLD" "$ROOT/state"
  : >"$WORLD/site-up"
  : >"$WORLD/online"
  : >"$WORLD/tunnel-up"
  echo running >"$WORLD/tunnel-state"
  echo healthy >"$WORLD/api-health"
  echo "$OLD" >"$WORLD/host"

  git init -q --bare -b main "$ROOT/origin.git"
  git init -q -b main "$ROOT/seed"
  echo "{\"destination\": \"https://$OLD/api/:path*\", \"health\": \"https://$OLD/healthz\"}" >"$ROOT/seed/vercel.json"
  git -C "$ROOT/seed" add .
  git -C "$ROOT/seed" -c user.name=t -c user.email=t@t.test commit -qm init
  git -C "$ROOT/seed" push -q "$ROOT/origin.git" main main:dev
}

# One cron run. git and flock are the real ones; docker and curl the stand-ins.
run() {
  PATH="$ROOT/bin:$PATH" bash -c "
    export HOME='$ROOT/home'
    source <(sed -e 's#^export PATH=.*##' -e 's#^main\$##' '$HERE/tunnel-watch.sh')
    main
  "
}

export WERA_SITE=https://site.test WERA_ONLINE_URL=https://online.test
export WERA_FRONTEND_REPO=$ROOT/origin.git WERA_WATCH_STATE=$ROOT/state
export WERA_WATCH_PATIENCE=3 WERA_WATCH_RESTART_GAP=300 WERA_WATCH_SETTLE=0
export WERA_TEST_NEW=$NEW

restarts() { [ -e "$WORLD/restarts" ] && wc -l <"$WORLD/restarts" | tr -d ' ' || echo 0; }
commits() { git -C "$ROOT/origin.git" rev-list --count "$1"; }
address() { git -C "$ROOT/origin.git" show "$1:vercel.json"; }

expect() {
  if [ "$2" = "$3" ]; then
    echo "ok: $1"
  else
    echo "FAIL: $1: wanted '$2', got '$3'"
    FAILED=1
  fi
}

fresh
run
expect "a site that answers is left alone" 0 "$(restarts)"
expect "...and nothing is committed" 1 "$(commits main)"

fresh
rm "$WORLD/site-up" "$WORLD/online" "$WORLD/tunnel-up"
echo "$STUCK" >"$WORLD/stuck"
run; run; run; run
expect "offline: the tunnel is left alone, even looking stuck" 0 "$(restarts)"
expect "offline: said once" 1 "$(grep -c offline "$ROOT/state/watch.log")"

fresh
rm "$WORLD/site-up"
echo "$NEW" >"$WORLD/host"
run
expect "a tunnel Vercel does not know: main gets its address" 2 "$(commits main)"
expect "...and so does dev" 2 "$(commits dev)"
expect "...in every rewrite" 0 "$(address main | grep -c "$OLD")"
expect "...and the tunnel keeps running" 0 "$(restarts)"
run
expect "a slow Vercel deploy is not pushed twice" 2 "$(commits main)"

fresh
rm "$WORLD/site-up"
echo "$NEW" >"$WORLD/host"
git -C "$ROOT/seed" checkout -q -b fix
sed -i "s/$OLD/$NEW/g" "$ROOT/seed/vercel.json"
git -C "$ROOT/seed" -c user.name=t -c user.email=t@t.test commit -qam "by hand"
git -C "$ROOT/seed" push -q "$ROOT/origin.git" fix:main
run
expect "main already right: left as it is" 2 "$(commits main)"
expect "...and dev still catches up" 2 "$(commits dev)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo "$OLD_STUCK" >"$WORLD/stuck"
run
expect "older cloudflared's wording counts too" 1 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo 'ERR Failed to dial a quic connection error="timeout: no recent network activity"' >"$WORLD/stuck"
run
expect "any other error gets time to recover" 0 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo "$STUCK" >"$WORLD/stuck"
run
expect "a stuck tunnel is replaced on sight" 1 "$(restarts)"
echo "$STUCK" >"$WORLD/stuck"
run
expect "...but not again within the gap" 1 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo "$STUCK" >"$WORLD/stuck"
: >"$WORLD/restart-heals"
WERA_WATCH_SETTLE=5 run
expect "a new tunnel that answers at once is published in the same run" 2 "$(commits main)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
run; run
expect "a failing tunnel gets time to recover" 0 "$(restarts)"
run
expect "...then is replaced" 1 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo "$STUCK" >"$WORLD/stuck"
echo unhealthy >"$WORLD/api-health"
run; run; run; run
expect "the API down is not the tunnel's fault" 0 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo exited >"$WORLD/tunnel-state"
run; run; run; run
expect "a tunnel stopped on purpose stays stopped" 0 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
echo "$STUCK" >"$WORLD/stuck"
: >"$ROOT/state/paused"
run
expect "paused means paused" 0 "$(restarts)"

fresh
rm "$WORLD/site-up" "$WORLD/tunnel-up"
run
: >"$WORLD/site-up"
run
expect "the site coming back clears the count" no "$([ -e "$ROOT/state/failed-checks" ] && echo yes || echo no)"
expect "...and says how long it was down" 1 "$(grep -c 'site is back' "$ROOT/state/watch.log")"

exit "$FAILED"
