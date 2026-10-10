#!/usr/bin/env bash
# Fails when a commit subject in the range is not a Conventional Commit:
#   type(scope)!: summary   with type one of the list below.
# Usage: check-commits.sh <base-ref> <head-ref>
set -euo pipefail
base=$1 head=$2
pattern='^(feat|fix|perf|refactor|docs|test|build|ci|chore|style|revert)(\([a-z0-9._/-]+\))?!?: [^ ].{2,}$'
allow=$(grep -oE '^[0-9a-f]{7,40}' .conventional-commits-allow 2>/dev/null || true)
bad=0
while IFS=$'\t' read -r sha subject; do
  case $subject in Merge\ *) continue ;; esac   # merge commits are fine
  if grep -q "^${sha:0:7}" <<<"$allow"; then continue; fi
  if ! grep -qE "$pattern" <<<"$subject"; then
    echo "::error::${sha:0:7} is not a Conventional Commit: \"$subject\""
    bad=1
  fi
done < <(git log --format=$'%H\t%s' "$base..$head")
if [ "$bad" = 1 ]; then
  echo "Use type(scope): summary, e.g. \"feat(jobs): hide a company\" or \"fix: handle empty boards\"."
  exit 1
fi
echo "All commit subjects follow Conventional Commits."
