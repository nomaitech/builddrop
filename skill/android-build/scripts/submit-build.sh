#!/usr/bin/env bash
# Submit an Android project to the build server and wait for the result.
# Usage: submit-build.sh [--no-wait] [--force] <repo-path> [gradle-task] [name]
set -euo pipefail

SERVER="${ANDROID_BUILD_SERVER:-http://localhost:8085}"
SERVER="${SERVER%/}"
WAIT=1
FORCE=0
while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --no-wait) WAIT=0 ;;
    --force) FORCE=1 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

REPO="$(cd "${1:-.}" && pwd)"
TASK="${2:-assembleDebug}"
NAME="${3:-}"

auth=()
if [[ -n "${ANDROID_BUILD_SERVER_TOKEN:-}" ]]; then
  auth=(-H "Authorization: Bearer $ANDROID_BUILD_SERVER_TOKEN")
fi

json() { python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get(sys.argv[1]) or "")' "$1"; }

if ! curl -fsS "$SERVER/api/health" >/dev/null 2>&1; then
  echo "ERROR: build server not reachable at $SERVER" >&2
  echo "Start it with: systemctl --user start builddrop-server" >&2
  exit 2
fi

payload="$(python3 -c 'import json,sys; print(json.dumps({"repo_path": sys.argv[1], "task": sys.argv[2], "name": sys.argv[3], "force": sys.argv[4] == "1"}))' "$REPO" "$TASK" "$NAME" "$FORCE")"
resp="$(curl -sS "${auth[@]}" -H 'Content-Type: application/json' -d "$payload" "$SERVER/api/builds")"
id="$(echo "$resp" | json id)"
if [[ -z "$id" ]]; then
  echo "ERROR: build was not accepted: $resp" >&2
  exit 1
fi
if [[ "$(echo "$resp" | json duplicate)" == "True" ]]; then
  echo "No source changes since build $id; reusing it instead of building again (use --force to rebuild)."
else
  echo "Queued build $id for $REPO ($TASK)"
fi
[[ $WAIT -eq 0 ]] && exit 0

status=""
while true; do
  build="$(curl -fsS "$SERVER/api/builds/$id")"
  new_status="$(echo "$build" | json status)"
  if [[ "$new_status" != "$status" ]]; then echo "Status: $new_status"; status="$new_status"; fi
  case "$status" in
    success|failed) break ;;
  esac
  sleep 3
done

echo "$build" | python3 -m json.tool
if [[ "$status" == "failed" ]]; then
  echo "----- last 60 lines of build log -----"
  curl -fsS "$SERVER/api/builds/$id/log" | tail -n 60
  exit 1
fi
echo "APK ready: $SERVER/api/builds/$id/apk"
