# shellcheck shell=bash
# The typed result of a check (change typed-boundaries): ygg.check/v1, written by ygg-check-result, the converter of
# the work image, from the machine output of the check's tool. A check script sources this file, names itself with
# ygg_start, says at each step what the end of the script should convert (ygg_stage, ygg_tool_stage) and does
# nothing else: an EXIT trap calls ygg_result with the script's exit code. When YGG_CHECK_RESULT is unset (a person's
# run, a command that asked for no result) nothing here runs a tool or writes a file, and the script ends as it
# always did. The exit code stays the script's own, with one exception: when the converter fails the script exits
# non-zero even if its tool passed, a failure of the realm, not a finding.
#
#   ygg_start NAME                      the check's name as pack.yaml gives it; makes $YGG_TMP, which is removed at the end
#   ygg_typed                           true when a result was asked for
#   ygg_stage FORMAT INPUT...           what to convert if the script ends now; FORMAT none takes KIND MESSAGE pairs
#   ygg_tool_stage FORMAT KIND MSG FILE...   FORMAT on the files when they all hold output, else the one message
#   ygg_render_test_json FILE           prints what go test -json says as go test would, for the log

ygg_typed() { [ -n "${YGG_CHECK_RESULT:-}" ]; }

ygg_now_ms() {
  local t
  t="$(date +%s%3N 2>/dev/null)" || t=""
  [[ "$t" =~ ^[0-9]+$ ]] || t=$((SECONDS * 1000))
  printf '%s' "$t"
}

ygg_start() {
  YGG_CHECK_NAME="$1"
  YGG_T0="$(ygg_now_ms)"
  YGG_TMP="$(mktemp -d)"
  YGG_FORMAT=none
  YGG_INPUTS=()
  trap ygg_exit EXIT
}

ygg_stage() {
  YGG_FORMAT="$1"
  shift
  YGG_INPUTS=("$@")
}

ygg_tool_stage() {
  local format="$1" kind="$2" message="$3" f
  shift 3
  for f in "$@"; do
    if [ ! -s "$f" ]; then
      ygg_stage none "$kind" "$message"
      return 0
    fi
  done
  ygg_stage "$format" "$@"
}

# ygg_result FORMAT INPUT...: the first command of the EXIT trap, whose $? is the script's exit code. It returns that
# code, or 1 when the converter failed and the code was 0.
ygg_result() {
  local code=$?
  ygg_typed || return "$code"
  local format="$1" converter
  shift
  local -a args=(--check "$YGG_CHECK_NAME" --exit "$code" --duration-ms "$(($(ygg_now_ms) - YGG_T0))" --format "$format")
  # The script's own timeout ends a command with 124.
  [ "$code" -ne 124 ] || args+=(--timed-out)
  if [ "$format" = none ]; then
    while [ "$#" -ge 2 ]; do
      args+=(--kind "$1" --message "$2")
      shift 2
    done
  else
    local f
    for f in "$@"; do args+=(--input "$f"); done
  fi
  converter="$(command -v ygg-check-result || true)"
  [ -n "$converter" ] || converter=/opt/ygg/bin/ygg-check-result
  if ! "$converter" "${args[@]}"; then
    [ "$code" -eq 0 ] && return 1
  fi
  return "$code"
}

ygg_exit() {
  ygg_result "$YGG_FORMAT" "${YGG_INPUTS[@]}" && YGG_RC=0 || YGG_RC=$?
  rm -rf -- "$YGG_TMP"
  exit "$YGG_RC"
}

# go test -json holds the text go test prints, escaped, one event to a line. The log shows it again, without the
# "=== RUN" lines and the "--- PASS" lines that -json adds (it implies -v).
ygg_render_test_json() {
  awk '
    function unescape(s,   out, i, n, c, h) {
      out = ""; n = length(s)
      for (i = 1; i <= n; i++) {
        c = substr(s, i, 1)
        if (c != "\\") { out = out c; continue }
        i++; c = substr(s, i, 1)
        if (c == "n") out = out "\n"
        else if (c == "t") out = out "\t"
        else if (c == "r") out = out "\r"
        else if (c == "u") {
          h = substr(s, i + 1, 4); i += 4
          if (h == "003c") out = out "<"
          else if (h == "003e") out = out ">"
          else if (h == "0026") out = out "&"
          else out = out "\\u" h
        } else out = out c
      }
      return out
    }
    match($0, /"Output":"/) {
      s = substr($0, RSTART + RLENGTH)
      sub(/"\}$/, "", s)
      s = unescape(s)
      if (s ~ /^[ ]*=== / || s ~ /^[ ]*--- PASS/) next
      printf "%s", s
    }
  ' "$1"
}
