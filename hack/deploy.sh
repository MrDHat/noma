#!/usr/bin/env bash
# Builds Noma's images inside the dev cluster and rolls out noma-server.
# make dev-up runs this; run it again (make deploy) after changing the code.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
kc() { kubectl --context "$PROFILE" "$@"; }

arch="$(minikube -p "$PROFILE" ssh -- uname -m | tr -d '\r')"
case "$arch" in
  aarch64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) echo "unsupported node architecture: $arch" >&2; exit 1 ;;
esac

out="${ROOT}/bin/linux-${goarch}"
make -C "$ROOT" build GOOS=linux GOARCH="$goarch" BIN="$out"

ctx="$(mktemp -d)"
trap 'rm -rf "$ctx"' EXIT
cp "${out}/noma-server" "$ctx/"
docker build -q --platform "linux/${goarch}" -t noma-server:dev -f "${ROOT}/build/server.Dockerfile" "$ctx"
minikube -p "$PROFILE" image load --overwrite noma-server:dev

kc apply -f "${ROOT}/deploy/dev/rbac.yaml" -f "${ROOT}/deploy/dev/server.yaml"
kc -n noma-system rollout restart deployment/noma-server
kc -n noma-system rollout status deployment/noma-server --timeout=120s
