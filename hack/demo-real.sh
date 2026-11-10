#!/usr/bin/env bash
# Runs the M3 demo against real Claude Code and a real GitHub token. It
# needs you: you sign in to Claude once, ask Claude for changes from your
# phone, and give a token that can push to your fork of the noma repo.
# Run it to accept a milestone and before each release; nothing here goes
# to CI.
#
#   DEMO_NOMA_REPO  https URL of a noma repo you can push to
#                   (default https://github.com/MrDHat/noma.git)
#   DEMO_GH_TOKEN   a GitHub token that can push there (asked for if unset)
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
REPO="${DEMO_REPO:-https://github.com/octocat/Hello-World.git}"
NOMA_REPO="${DEMO_NOMA_REPO:-https://github.com/MrDHat/noma.git}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
kc() { kubectl --context "$PROFILE" "$@"; }
step() { printf '\n==> %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
pause() { read -r -p "$1 Press Enter to go on... " _; }

NOMA_CLAUDE=real "${ROOT}/hack/dev-up.sh"
make -C "$ROOT" build >/dev/null
noma="${ROOT}/bin/noma"
for ws in demo-a demo-b demo-noma; do "$noma" rm "$ws" >/dev/null 2>&1 || true; done

# pod_of NAME prints the pod of the workspace called NAME.
pod_of() {
  kc -n noma-u-dev get pods -o name | while read -r p; do
    if kc -n noma-u-dev exec "${p#pod/}" -- sh -c "test -d /workspaces/*/$1" 2>/dev/null; then
      echo "${p#pod/}"
    fi
  done
}
# link_of NAME prints the workspace's Remote Control link from noma ls.
link_of() { "$noma" ls | awk -v n="$1" '$1 == n { print $5 }'; }
# wait_ready NAME waits until noma ls shows the workspace ready.
wait_ready() {
  for _ in $(seq 1 120); do
    [ "$("$noma" ls | awk -v n="$1" '$1 == n { print $2 }')" = ready ] && return 0
    sleep 2
  done
  fail "$1 did not get back to ready"
}

step "1. Sign in once: demo-a asks for a code, demo-b starts signed in"
"$noma" up --name demo-a "$REPO"
out="$("$noma" up --name demo-b "$REPO" </dev/null 2>&1)" || fail "noma up demo-b: $out"
echo "$out"
grep -q "Paste code" <<<"$out" && fail "demo-b asked to sign in again"

step "2. noma ls and noma attach"
"$noma" ls
"$noma" attach demo-a

step "3. Stop and start keep the files and the link"
pod_b="$(pod_of demo-b)"
link_b="$(link_of demo-b)"
kc -n noma-u-dev exec "$pod_b" -- sh -c 'touch /workspaces/*/demo-b/uncommitted-work'
"$noma" stop demo-b
"$noma" start demo-b
[ "$(link_of demo-b)" = "$link_b" ] || fail "demo-b's link changed after stop and start"
kc -n noma-u-dev exec "$pod_b" -- sh -c 'test -f /workspaces/*/demo-b/uncommitted-work' ||
  fail "demo-b lost its uncommitted file"
echo "OK: same link, file kept"

marker="noma-demo-$(date +%s).txt"
cat <<MSG

Open demo-b's link in the Claude app on your phone. It should open a
conversation named demo-b, not a list of environments to pick from. Send:

  Create a file named ${marker} in the repo containing "hello from my phone".

Approve the write if Claude asks.
MSG
pause "Once Claude says it's done."
kc -n noma-u-dev exec "$pod_b" -- sh -c "test -f /workspaces/*/demo-b/${marker}" ||
  fail "${marker} is not in demo-b"
echo "OK: Claude created ${marker}"

step "4. Kill Claude: it restarts by itself with the same link"
pod_a="$(pod_of demo-a)"
link_a="$(link_of demo-a)"
kc -n noma-u-dev exec "$pod_a" -- bash -c 'for p in /proc/[0-9]*; do
  [ "$(tr "\0" " " <"$p/cmdline" 2>/dev/null | cut -c1-21)" = "claude remote-control" ] && kill -KILL "${p#/proc/}"
done; true'
sleep 5
wait_ready demo-a
[ "$(link_of demo-a)" = "$link_a" ] || fail "demo-a's link changed after Claude restarted"
"$noma" ls
echo "OK: Claude restarted; the session is still at ${link_a}"

step "5. Delete the pod: it comes back on the same volume"
kc -n noma-u-dev delete pod "$pod_a"
wait_ready demo-a
[ "$(link_of demo-a)" = "$link_a" ] || fail "demo-a's link changed after its pod was deleted"
echo "OK: demo-a is back with the same link"

step "6. A secret, the noma repo on its own image, and a push"
token="${DEMO_GH_TOKEN:-}"
if [ -z "$token" ]; then
  read -r -s -p "GitHub token that can push to ${NOMA_REPO}: " token
  echo
fi
repo_path="$(sed -E 's#^https://##; s#\.git$##' <<<"$NOMA_REPO")"
printf '%s\n' "$token" | "$noma" secret set demo-gh --git-host github.com --env GH_TOKEN --repos "$repo_path"
"$noma" secret ls
"$noma" up --name demo-noma "$NOMA_REPO"
pod_n="$(pod_of demo-noma)"
image="$(kc -n noma-u-dev get pod "$pod_n" -o jsonpath='{.spec.containers[0].image}')"
echo "demo-noma runs ${image}"
branch="noma-demo-$(date +%s)"
kc -n noma-u-dev exec "$pod_n" -- sh -c "cd /workspaces/*/demo-noma &&
  git switch -c ${branch} && git commit --allow-empty -qm 'Noma M3 demo' && git push -q origin ${branch}"
git ls-remote --exit-code "$NOMA_REPO" "refs/heads/${branch}" >/dev/null || fail "${branch} did not reach ${NOMA_REPO}"
echo "OK: pushed ${branch} from the workspace; delete it with: git push ${NOMA_REPO} --delete ${branch}"
kc -n noma-u-dev exec "$pod_n" -- sh -c 'test -z "$(ls -A ~/.git-credentials 2>/dev/null)"' ||
  fail "the token was written to the workspace volume"

step "Clean up"
"$noma" secret rm demo-gh
for ws in demo-a demo-b demo-noma; do "$noma" rm "$ws"; done
echo
echo "M3 demo passed."
