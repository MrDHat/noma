#!/usr/bin/env bash
# Keeps the lifecycle model (verify/lifecycle) in step with the code it
# describes. verify/lifecycle/covered.sha256 holds the SHA-256 of each file
# the model covers, as of the last time someone confirmed the model matches.
#
#   hack/lifecycle-sync.sh check    fail if a covered file changed since then
#   hack/lifecycle-sync.sh update   record the current files as confirmed
#
# Run update only after reading the diff and updating the model to match;
# .claude/skills/verify-workspace-lifecycle/SKILL.md says how.
set -euo pipefail
cd "$(dirname "$0")/.."

SUMS=verify/lifecycle/covered.sha256

case ${1:-} in
check)
  changed=$(shasum -a 256 -c "$SUMS" 2>&1 | grep -v ': OK$' || true)
  if [[ -n $changed ]]; then
    since=$(git log -1 --format=%H -- "$SUMS" 2>/dev/null || true)
    echo "The lifecycle model may no longer match these files:"
    sed 's/^/  /' <<<"$changed"
    echo
    echo "Follow .claude/skills/verify-workspace-lifecycle/SKILL.md: review the change"
    echo "against verify/lifecycle/WorkspaceLifecycle.tla, update the model if needed,"
    echo "run make verify-lifecycle, then hack/lifecycle-sync.sh update and commit the result."
    [[ -n $since ]] && echo "Last confirmed at ${since}: git diff $since -- $(awk '{printf "%s ", $2}' "$SUMS")"
    exit 1
  fi
  echo "lifecycle model is in sync with $(wc -l <"$SUMS" | tr -d ' ') covered files"
  ;;
update)
  files=$(awk '{print $2}' "$SUMS")
  # shellcheck disable=SC2086 # one path per word
  shasum -a 256 $files >"$SUMS.tmp"
  mv "$SUMS.tmp" "$SUMS"
  echo "recorded $(wc -l <"$SUMS" | tr -d ' ') covered files in $SUMS"
  ;;
*)
  echo "usage: $0 check|update" >&2
  exit 2
  ;;
esac
