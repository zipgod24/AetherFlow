#!/usr/bin/env bash
# Seed the AetherFlow corpus with the bundled threat-intel snippets and runbooks.
#
# Requires the gateway to be up. Reads from examples/corpus/*.md and posts each
# as one document to /v1/corpus.

set -euo pipefail
GW="${AETHER_GATEWAY:-http://localhost:8080}"

shopt -s nullglob
files=(examples/corpus/*.md)
if [ ${#files[@]} -eq 0 ]; then
  echo "no files in examples/corpus/"
  exit 1
fi

for f in "${files[@]}"; do
  title=$(head -n 1 "$f" | sed -e 's/^#\+ *//' -e 's/"/\\"/g')
  src=$(basename "$f" .md | awk -F. '{print $1}')
  body=$(jq -Rs . < "$f")
  echo ">> ingesting $f as $src/$title"
  curl -sS -X POST "$GW/v1/corpus" \
    -H 'content-type: application/json' \
    -d "$(printf '{"source":"%s","title":"%s","text":%s}' "$src" "$title" "$body")" \
    | jq .
done
echo "done."
