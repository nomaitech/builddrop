#!/usr/bin/env bash
# Submit an Android project to the build server and wait for the result.
# Usage: submit-build.sh [--no-wait] <repo-path> [gradle-task] [name]
set -euo pipefail

SERVER="${ANDROID_BUILD_SERVER:-http://localhost:8085}"
SERVER="${SERVER%/}"
WAIT=1
if [[ "${1:-}" == "--no-wait" ]]; then WAIT=0; shift; fi

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
  echo "Start it with: systemctl --user start android-build-server" >&2
  exit 2
fi

payload="$(python3 -c 'import json,sys; print(json.dumps({"repo_path": sys.argv[1], "task": sys.argv[2], "name": sys.argv[3]}))' "$REPO" "$TASK" "$NAME")"
resp="$(curl -sS "${auth[@]}" -H 'Content-Type: application/json' -d "$payload" "$SERVER/api/builds")"
id="$(echo "$resp" | json id)"
if [[ -z "$id" ]]; then
  echo "ERROR: build was not accepted: $resp" >&2
  exit 1
fi
echo "Queued build $id for $REPO ($TASK)"
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
