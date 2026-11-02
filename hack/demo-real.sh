#!/usr/bin/env bash
# Runs the M2 demo against real Claude Code. It needs you: you sign in to
# Claude in your browser, then ask Claude to change the repo from your phone.
# Run it to accept a milestone and before each release.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
REPO="${DEMO_REPO:-https://github.com/octocat/Hello-World.git}"
NAME="${DEMO_NAME:-demo}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
kc() { kubectl --context "$PROFILE" "$@"; }

NOMA_CLAUDE=real "${ROOT}/hack/dev-up.sh"
make -C "$ROOT" build >/dev/null
noma="${ROOT}/bin/noma"

echo
echo "==> noma up ${REPO}"
"$noma" up --name "$NAME" "$REPO"

marker="noma-demo-$(date +%s).txt"
cat <<MSG

==> Phone check
Open the link above in the Claude app on your phone and send:

  Create a file named ${marker} in the repo containing "hello from my phone".

Approve the write if Claude asks.
MSG
read -r -p "Press Enter once Claude says it's done... " _

pod="$(kc -n noma-u-dev get pods -o name | while read -r p; do
  if kc -n noma-u-dev exec "${p#pod/}" -- sh -c "test -f /workspaces/*/${NAME}/${marker}" 2>/dev/null; then
    echo "${p#pod/}"
  fi
done)"
if [ -z "$pod" ]; then
  echo "FAIL: ${marker} is not in the workspace" >&2
  exit 1
fi
echo "OK: Claude created ${marker} in ${pod}"

echo
echo "==> noma rm ${NAME}"
"$noma" rm "$NAME"
echo
echo "Demo passed."
