#!/usr/bin/env bash
# Builds Noma's images, loads them into the dev cluster and rolls out
# noma-server. make dev-up runs this; run it again (make deploy) after
# changing the code.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
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

# image <name> <dockerfile> <files...>: builds from a context holding only the files.
image() {
  local name="$1" dockerfile="$2" ctx
  shift 2
  ctx="$(mktemp -d)"
  cp "$@" "$ctx/"
  docker build -q --platform "linux/${goarch}" -t "${name}:dev" -f "$dockerfile" "$ctx" >/dev/null
  rm -rf "$ctx"
  minikube -p "$PROFILE" image load --overwrite "${name}:dev"
  echo "loaded ${name}:dev"
}
image noma-server "${ROOT}/build/server.Dockerfile" "${out}/noma-server"
image noma-workspace "${ROOT}/build/workspace.Dockerfile" "${out}/noma-agent" "${CERTS}/ca.crt"

kc apply -f "${ROOT}/deploy/dev/rbac.yaml" -f "${ROOT}/deploy/dev/server.yaml"
kc -n noma-system rollout restart deployment/noma-server
kc -n noma-system rollout status deployment/noma-server --timeout=120s
