#!/usr/bin/env bash
# Builds Noma's images inside the dev cluster and rolls out noma-server.
# make dev-up runs this; run it again (make deploy) after changing the code.
# NOMA_CLAUDE=fake swaps the pinned Claude Code in the workspace image for
# the fake one the e2e tests use.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
CLAUDE="${NOMA_CLAUDE:-real}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CERTS="${ROOT}/.dev/certs"
kc() { kubectl --context "$PROFILE" "$@"; }

arch="$(minikube -p "$PROFILE" ssh -- uname -m | tr -d '\r')"
case "$arch" in
  aarch64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) echo "unsupported node architecture: $arch" >&2; exit 1 ;;
esac

# The agent endpoint's TLS certificate, from a CA only workspaces trust.
if [ ! -f "${CERTS}/ca.crt" ]; then
  go run "${ROOT}/hack/devcerts" "$CERTS"
fi
# The server hands the CA to workspaces (a ConfigMap per user namespace).
kc -n noma-system create secret generic noma-server-tls --type=kubernetes.io/tls \
  --from-file=tls.crt="${CERTS}/tls.crt" --from-file=tls.key="${CERTS}/tls.key" \
  --from-file=ca.crt="${CERTS}/ca.crt" --dry-run=client -o yaml | kc apply -f -

out="${ROOT}/bin/linux-${goarch}"
make -C "$ROOT" build GOOS=linux GOARCH="$goarch" BIN="$out"
CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -o "${out}/fakeclaude" "${ROOT}/test/fakeclaude"

# image NAME DOCKERFILE FILE... [-- BUILD-OPT...] builds NAME:dev inside the
# node, from a context holding only the given files.
image() {
  local name="$1" ctx
  ctx="$(mktemp -d)"
  cp "$2" "$ctx/Dockerfile"
  shift 2
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do cp "$1" "$ctx/"; shift; done
  [ $# -gt 0 ] && shift
  # minikube exits 0 even when the build fails, so check that it named the image.
  local log
  log="$(minikube -p "$PROFILE" image build -t "${name}:dev" "$@" "$ctx" 2>&1)" || true
  rm -rf "$ctx"
  if ! grep -q "naming to .*${name}:dev" <<<"$log"; then
    echo "$log" >&2
    echo "building ${name}:dev failed" >&2
    exit 1
  fi
  echo "built ${name}:dev"
}
image noma-server "${ROOT}/build/server.Dockerfile" "${out}/noma-server"
image noma-workspace "${ROOT}/build/workspace.Dockerfile" \
  "${out}/noma-agent" "${out}/fakeclaude" -- --build-opt="opt=build-arg:CLAUDE=${CLAUDE}"

kc apply -f "${ROOT}/deploy/dev/rbac.yaml" -f "${ROOT}/deploy/dev/server.yaml"
# Real Claude needs room for the Remote Control server and its sessions; the
# fake one needs almost nothing, so tests can run several workspaces.
if [ "$CLAUDE" = real ]; then memory=2Gi; else memory=256Mi; fi
if [ "$CLAUDE" = real ]; then fake=0; else fake=1; fi
kc -n noma-system set env deployment/noma-server NOMA_WORKSPACE_MEMORY="$memory" NOMA_FAKE_CLAUDE="$fake" >/dev/null
kc -n noma-system rollout restart deployment/noma-server
kc -n noma-system rollout status deployment/noma-server --timeout=120s
