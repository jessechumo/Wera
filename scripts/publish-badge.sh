#!/usr/bin/env bash
# Publishes a shields.io endpoint badge as JSON on the orphan "badges"
# branch, so README badges need no third-party service:
#   https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/<repo>/badges/<name>.json
# Usage: publish-badge.sh <name> <message> <percent>
set -euo pipefail
name=$1 message=$2 pct=${3%.*}
color=red
if   [ "$pct" -ge 85 ]; then color=brightgreen
elif [ "$pct" -ge 75 ]; then color=green
elif [ "$pct" -ge 65 ]; then color=yellowgreen
elif [ "$pct" -ge 50 ]; then color=yellow
fi
dir=$(mktemp -d)
git config --global user.name "github-actions[bot]"
git config --global user.email "41898282+github-actions[bot]@users.noreply.github.com"
if git ls-remote --exit-code origin badges >/dev/null 2>&1; then
  git clone --quiet --depth 1 --branch badges "https://x-access-token:${GH_TOKEN}@github.com/${GITHUB_REPOSITORY}.git" "$dir"
else
  git init --quiet "$dir" && git -C "$dir" checkout --quiet --orphan badges
  git -C "$dir" remote add origin "https://x-access-token:${GH_TOKEN}@github.com/${GITHUB_REPOSITORY}.git"
fi
printf '{"schemaVersion":1,"label":"%s","message":"%s","color":"%s"}\n' "$name" "$message" "$color" > "$dir/$name.json"
git -C "$dir" add "$name.json"
git -C "$dir" diff --cached --quiet && exit 0
git -C "$dir" commit --quiet -m "chore: update $name badge to $message"
git -C "$dir" push --quiet origin badges
