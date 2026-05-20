#!/bin/sh
set -eu

dotvault_script_dir() {
  dotvault_script_path=$1
  case $dotvault_script_path in
    */*)
      dotvault_script_dir=${dotvault_script_path%/*}
      if [ -z "$dotvault_script_dir" ]; then
        dotvault_script_dir=/
      fi
      ;;
    *)
      dotvault_script_dir=.
      ;;
  esac

  case $dotvault_script_dir in
    /*|./*|../*|.) ;;
    *) dotvault_script_dir=./$dotvault_script_dir ;;
  esac

  CDPATH= cd "$dotvault_script_dir" && pwd
}

DOTVAULT_HOOK_DIR=${DOTVAULT_HOOK_DIR:-$(dotvault_script_dir "$0")}

dotvault_python() {
  if command -v python3.11 >/dev/null 2>&1; then
    command -v python3.11
    return 0
  fi
  command -v python3
}

dotvault_detect_agent() {
  payload_file=$1
  "$(dotvault_python)" - "$payload_file" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)

transcript_path = str(payload.get("transcript_path", ""))
if ".factory/" in transcript_path and transcript_path.endswith(".jsonl"):
    print("factory")
elif payload.get("platform") == "amp" or payload.get("amp_thread_id"):
    print("amp")
elif payload.get("session_id") and not payload.get("transcript_path"):
    print("hermes")
else:
    print("claude")
PY
}

dotvault_dispatch_hook() {
  event=$1
  payload_file=$(mktemp "${TMPDIR:-/tmp}/dotvault-hook.XXXXXX")
  trap 'rm -f "$payload_file"' EXIT HUP INT TERM

  cat >"$payload_file"
  if [ ! -s "$payload_file" ]; then
    echo "dotvault hook ${event}: expected JSON payload on stdin" >&2
    return 2
  fi

  agent=$(dotvault_detect_agent "$payload_file")
  case "$agent" in
    amp|claude|factory|hermes) ;;
    *)
      echo "dotvault hook ${event}: unsupported agent '${agent}'" >&2
      return 2
      ;;
  esac

  DOTVAULT_HOOK_EVENT=$event DOTVAULT_AGENT=$agent \
    "$(dotvault_python)" "$DOTVAULT_HOOK_DIR/lib/${agent}_digest.py" <"$payload_file"
}
