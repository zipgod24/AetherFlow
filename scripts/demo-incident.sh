#!/usr/bin/env bash
# Submit the canonical demo incident and tail its events.

set -euo pipefail
GW="${AETHER_GATEWAY:-http://localhost:8080}"

resp=$(curl -sS -X POST "$GW/v1/incidents" \
  -H 'content-type: application/json' \
  -d @examples/incidents/suspicious-domain.json)

echo "$resp"
id=$(echo "$resp" | jq -r .incident_id)
echo
echo "watching incident $id (Ctrl-C to stop)…"
echo

curl -sN "$GW/v1/events?incident=$id" | while IFS= read -r line; do
  case "$line" in
    "event: "*)  evname="${line#event: }" ;;
    "data: "*)   echo "[$evname] ${line#data: }" ;;
  esac
done
