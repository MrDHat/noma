#!/usr/bin/env bash
# Builds Noma's images inside the dev cluster and rolls out noma-server.
# make dev-up runs this; run it again (make deploy) after changing the code.
# NOMA_CLAUDE=real puts the pinned Claude Code in the workspace image instead
# of the fake one the tests use.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
CLAUDE="${NOMA_CLAUDE:-fake}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CERTS="${ROOT}/.dev/certs"
kc() { kubectl --context "$PROFILE" "$@"; }

arch="$(minikube -p "$PROFILE" ssh -- uname -m | tr -d '\r')"
case "$arch" in
  aarch64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) echo "unsupported node architecture: $arch" >&2; exit 1 ;;
esac

# The agent endpoint's TLS certificate, from a CA only the workspace image trusts.
if [ ! -f "${CERTS}/ca.crt" ]; then
  go run "${ROOT}/hack/devcerts" "$CERTS"
fi
kc -n noma-system create secret tls noma-server-tls \
  --cert="${CERTS}/tls.crt" --key="${CERTS}/tls.key" --dry-run=client -o yaml | kc apply -f -

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
  "${out}/noma-agent" "${out}/fakeclaude" "${CERTS}/ca.crt" -- --build-opt="opt=build-arg:CLAUDE=${CLAUDE}"

kc apply -f "${ROOT}/deploy/dev/rbac.yaml" -f "${ROOT}/deploy/dev/server.yaml"
kc -n noma-system rollout restart deployment/noma-server
kc -n noma-system rollout status deployment/noma-server --timeout=120s
