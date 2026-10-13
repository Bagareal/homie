#!/usr/bin/env bash
# Writes the release notes of a tag to stdout: one section per commit since the
# previous tag (its first line as the title, the rest of the message below it).
# The release page shows them, and so does Homie when a new version is out.
# Usage: scripts/release-notes.sh v1.2.3
set -euo pipefail

tag="${1:?usage: release-notes.sh <tag>}"
prev="$(git describe --tags --abbrev=0 "${tag}^" 2>/dev/null || true)"
range="${prev:+$prev..}$tag"

for c in $(git rev-list --no-merges "$range"); do
  echo "### $(git log -1 --format=%s "$c")"
  echo
  body="$(git log -1 --format=%b "$c" | grep -vi '^co-authored-by:' || true)"
  if [ -n "$body" ]; then
    printf '%s\n\n' "$body"
  fi
done

if [ -n "$prev" ] && [ -n "${GITHUB_REPOSITORY:-}" ]; then
  echo "**Full changelog:** https://github.com/${GITHUB_REPOSITORY}/compare/${prev}...${tag}"
fi
