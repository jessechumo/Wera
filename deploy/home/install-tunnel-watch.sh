#!/usr/bin/env bash
# Installs tunnel-watch.sh on the home server for the current user, and
# updates it when run again.
#
# Needs: docker without sudo, and `gh` signed in to an account that can add
# deploy keys to the frontend repository. The key this adds can write to that
# one repository and nothing else, and works before anyone has logged in to
# the machine, which a desktop keyring does not.
set -euo pipefail

FRONTEND=${WERA_FRONTEND_SLUG:-jessechumo/Wera-Frontend}
KEY=${WERA_DEPLOY_KEY:-$HOME/.config/wera/tunnel-deploy-key}
BIN=$HOME/.local/bin/wera-tunnel-watch
LINE="* * * * * $BIN"

mkdir -p "$(dirname "$BIN")" "$(dirname "$KEY")"
chmod 700 "$(dirname "$KEY")"
install -m 755 "$(dirname "$0")/tunnel-watch.sh" "$BIN"

if [ ! -f "$KEY" ]; then
  ssh-keygen -q -t ed25519 -N '' -C wera-tunnel-watch -f "$KEY"
  gh repo deploy-key add "$KEY.pub" --repo "$FRONTEND" --allow-write --title wera-tunnel-watch
fi

{ crontab -l 2>/dev/null | grep -vF "$BIN" || true; echo "$LINE"; } | crontab -
echo "installed: $BIN runs every minute; its log is ~/.local/state/wera-tunnel/watch.log"
