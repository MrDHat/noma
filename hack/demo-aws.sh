#!/usr/bin/env bash
# Runs the M3.5 demo on EKS in your AWS account: from an empty account to a
# working Noma, the M3 demos on EKS, the isolation checks, and teardown. It
# needs you: you sign in to Claude once, use Claude from your phone, and
# give a token that can push to your fork of the noma repo. It costs money
# while it runs and ends with make aws-down; nothing here goes to CI.
#
#   AWS_PROFILE, AWS_REGION  the account and region (us-east-1)
#   DEMO_RELEASE    the Noma release to install, e.g. v0.4.0 (its chart on GHCR)
#   DEMO_NOMA_REPO  https URL of a noma repo you can push to
#                   (default https://github.com/MrDHat/noma.git)
#   DEMO_GH_TOKEN   a GitHub token that can push there (asked for if unset)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTEXT="${NOMA_AWS_CONTEXT:-noma-aws}"
RELEASE="${DEMO_RELEASE:?set DEMO_RELEASE to the Noma release to install, e.g. v0.4.0}"
REPO="${DEMO_REPO:-https://github.com/octocat/Hello-World.git}"
NOMA_REPO="${DEMO_NOMA_REPO:-https://github.com/MrDHat/noma.git}"
NS=noma-u-dev
kc() { kubectl --context "$CONTEXT" "$@"; }
step() { printf '\n==> %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; echo "The cluster is still up: fix and re-run, or take it down with make aws-down." >&2; exit 1; }
pause() { read -r -p "$1 Press Enter to go on... " _; }

# The CLI talks to the EKS install only, through its own config.
export NOMA_CONFIG="${ROOT}/bin/noma-aws.json"
(cd "$ROOT" && go build -o bin/noma ./cmd/noma)
noma="${ROOT}/bin/noma"

# pod_of NAME prints the pod of the workspace called NAME.
pod_of() {
  kc -n "$NS" get pods -o name | while read -r p; do
    if kc -n "$NS" exec "${p#pod/}" -- sh -c "test -d /workspaces/*/$1" 2>/dev/null; then
      echo "${p#pod/}"
    fi
  done
}
link_of() { "$noma" ls | awk -v n="$1" '$1 == n { print $5 }'; }
wait_ready() {
  for _ in $(seq 1 150); do
    [ "$("$noma" ls | awk -v n="$1" '$1 == n { print $2 }')" = ready ] && return 0
    sleep 2
  done
  fail "$1 did not get back to ready"
}

step "1. An empty account to a working Noma: make aws-up, helm install, noma connect"
"${ROOT}/hack/aws-up.sh"
helm --kube-context "$CONTEXT" upgrade --install noma oci://ghcr.io/mrdhat/charts/noma \
  --version "${RELEASE#v}" -n noma-system --create-namespace --wait --timeout 10m \
  --set workspace.storageClassName=noma-workspace --set postgres.storageClassName=noma-workspace \
  --set login.storageClassName=noma-login --set login.accessMode=ReadWriteMany
"$noma" connect --context "$CONTEXT"
# The preflight hook passed: gVisor runs on the node (containerd's config.d
# import works on AL2023), and the StorageClasses exist.
echo "OK: installed ${RELEASE}; the preflight check passed"

step "2. noma up the noma repo: setup progress, your Claude setup, one sign-in, the link"
[ -f "${HOME}/.claude/CLAUDE.md" ] || fail "put something in ~/.claude/CLAUDE.md first, so the copy can be checked"
token="${DEMO_GH_TOKEN:-}"
if [ -z "$token" ]; then
  read -r -s -p "GitHub token that can push to ${NOMA_REPO}: " token
  echo
fi
repo_path="$(sed -E 's#^https://##; s#\.git$##' <<<"$NOMA_REPO")"
printf '%s\n' "$token" | "$noma" secret set demo-gh --git-host github.com --env GH_TOKEN --repos "$repo_path"
out="$("$noma" up --name demo-noma "$NOMA_REPO" | tee /dev/stderr)"
grep -q "setup" <<<"$out" || fail "noma up showed no setup progress"
grep -Eq 'https://claude\.ai/code/session_[A-Za-z0-9]+' <<<"$out" || fail "noma up printed no session link"
pod_n="$(pod_of demo-noma)"
kc -n "$NS" exec "$pod_n" -- sh -c 'cat "$HOME/.claude/CLAUDE.md"' | cmp -s - "${HOME}/.claude/CLAUDE.md" ||
  fail "the workspace's CLAUDE.md is not yours"
# The volumes bound: the EBS CSI driver works with IMDS hop limit 1.
echo "OK: setup ran, your CLAUDE.md is in the workspace, and the link is above"

step "3. From your phone, with the laptop closed"
branch="noma-demo-aws-$(date +%s)"
cat <<MSG

Close the laptop lid if you like (the cluster keeps going). Open demo-noma's
link in the Claude app on your phone and send:

  Add a line saying "edited from my phone" to the end of README.md, run
  make test, commit, and push the branch ${branch}.

MSG
pause "Once Claude says it pushed."
git ls-remote --exit-code "$NOMA_REPO" "refs/heads/${branch}" >/dev/null || fail "${branch} did not reach ${NOMA_REPO}"
echo "OK: ${branch} is pushed; delete it with: git push ${NOMA_REPO} --delete ${branch}"
"$noma" rm demo-noma

step "4. The M3 demos on EKS"
"$noma" up --name demo-a "$REPO"
out="$("$noma" up --name demo-b "$REPO" </dev/null 2>&1)" || fail "noma up demo-b: $out"
grep -q "Paste code" <<<"$out" && fail "demo-b asked to sign in again; the login on EFS is not shared"
pod_a="$(pod_of demo-a)"
pod_b="$(pod_of demo-b)"
echo "OK: demo-b shares demo-a's login"

link_b="$(link_of demo-b)"
kc -n "$NS" exec "$pod_b" -- sh -c 'cd /workspaces/*/demo-b && touch uncommitted-work'
"$noma" stop demo-b
"$noma" start demo-b
[ "$(link_of demo-b)" = "$link_b" ] || fail "demo-b's link changed after stop and start"
kc -n "$NS" exec "$pod_b" -- sh -c 'test -f /workspaces/*/demo-b/uncommitted-work' || fail "demo-b lost its uncommitted file"
echo "OK: stop and start kept the file and the link"

link_a="$(link_of demo-a)"
kc -n "$NS" exec "$pod_a" -- bash -c 'for p in /proc/[0-9]*; do
  [ "$(tr "\0" " " <"$p/cmdline" 2>/dev/null | cut -c1-21)" = "claude remote-control" ] && kill -KILL "${p#/proc/}"
done; true'
sleep 5
wait_ready demo-a
[ "$(link_of demo-a)" = "$link_a" ] || fail "demo-a's link changed after Claude restarted"
echo "OK: Claude restarted with the same link"

kc -n "$NS" delete pod "$pod_a"
wait_ready demo-a
[ "$(link_of demo-a)" = "$link_a" ] || fail "demo-a's link changed after its pod was deleted"
echo "OK: the deleted pod came back on the same volume"

step "5. Isolation checks from inside demo-b"
pod_b="$(pod_of demo-b)"
node="$(kc -n "$NS" get pod "$pod_b" -o jsonpath='{.status.hostIP}')"
server="$(kc -n noma-system get pod -l app=noma-server -o jsonpath='{.items[0].status.podIP}')"
endpoint="$(kc config view --minify -o jsonpath='{.clusters[0].cluster.server}')"
kc -n "$NS" exec "$pod_b" -- noma-agent isolation-check --kubelet "$node" --pod "${server}:8080" --public-api "$endpoint" ||
  fail "the isolation checks failed"

step "6. Teardown refuses while workspaces exist, then removes everything"
if "${ROOT}/hack/aws-down.sh"; then
  fail "make aws-down went ahead with workspaces running"
fi
echo "OK: make aws-down refused while demo-a and demo-b exist"
"$noma" secret rm demo-gh
for ws in demo-a demo-b; do "$noma" rm "$ws"; done
for _ in $(seq 1 90); do
  [ "$("$noma" ls | awk 'NR > 1' | wc -l)" -eq 0 ] && break
  sleep 2
done
"${ROOT}/hack/aws-down.sh"
rm -f "$NOMA_CONFIG"
echo
echo "M3.5 demo passed."
