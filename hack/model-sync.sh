#!/usr/bin/env bash
# Keeps a TLA+ model under verify/ in step with the code it describes.
# verify/<model>/covered.sha256 holds the SHA-256 of each file the model
# covers, as of the last time someone confirmed the model matches.
#
#   hack/model-sync.sh lifecycle check    fail if a covered file changed since then
#   hack/model-sync.sh lifecycle update   record the current files as confirmed
#
# Run update only after reading the diff and updating the model to match;
# the model's verify skill under .claude/skills says how.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ $# -eq 2 && -f verify/$1/covered.sha256 ]] || { echo "usage: $0 <model under verify/> check|update" >&2; exit 2; }
SUMS=verify/$1/covered.sha256
skill=$(grep -l "verify/$1/" .claude/skills/*/SKILL.md | head -1)

case $2 in
check)
  changed=$(shasum -a 256 -c "$SUMS" 2>&1 | grep -v ': OK$' || true)
  if [[ -n $changed ]]; then
    since=$(git log -1 --format=%H -- "$SUMS" 2>/dev/null || true)
    echo "The $1 model may no longer match these files:"
    sed 's/^/  /' <<<"$changed"
    echo
    echo "Follow $skill: review the change against verify/$1, update the model"
    echo "if needed, run hack/verify-model.sh $1, then hack/model-sync.sh $1 update"
    echo "and commit the result."
    [[ -n $since ]] && echo "Last confirmed at ${since}: git diff $since -- $(awk '{printf "%s ", $2}' "$SUMS")"
    exit 1
  fi
  echo "$1 model is in sync with $(wc -l <"$SUMS" | tr -d ' ') covered files"
  ;;
update)
  files=$(awk '{print $2}' "$SUMS")
  # shellcheck disable=SC2086 # one path per word
  shasum -a 256 $files >"$SUMS.tmp"
  mv "$SUMS.tmp" "$SUMS"
  echo "recorded $(wc -l <"$SUMS" | tr -d ' ') covered files in $SUMS"
  ;;
*)
  echo "usage: $0 <model under verify/> check|update" >&2
  exit 2
  ;;
esac
