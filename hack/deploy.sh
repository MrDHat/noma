#!/usr/bin/env bash
# Builds Noma's images inside the dev cluster and rolls out noma-server.
# make dev-up runs this; run it again (make deploy) after changing the code.
# NOMA_CLAUDE=fake swaps the pinned Claude Code in the workspace image for
# the fake one the e2e tests use. NOMA_RELEASE=vX.Y.Z deploys that release's
# published images instead of building them (make daily-up).
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

# The agent endpoint's TLS certificate, from a CA only the workspace image trusts.
if [ ! -f "${CERTS}/ca.crt" ]; then
  go run "${ROOT}/hack/devcerts" "$CERTS"
fi
kc -n noma-system create secret tls noma-server-tls \
  --cert="${CERTS}/tls.crt" --key="${CERTS}/tls.key" --dry-run=client -o yaml | kc apply -f -

if [ -n "${NOMA_RELEASE:-}" ]; then
  # The release's images, by digest.
  images="$(curl -fsSL "https://github.com/MrDHat/noma/releases/download/${NOMA_RELEASE}/images.txt")"
  server_image="$(sed -n 's/^noma-server=//p' <<<"$images")"
  workspace_image="$(sed -n 's/^noma-workspace=//p' <<<"$images")"
  if [ -z "$server_image" ] || [ -z "$workspace_image" ]; then
    echo "release ${NOMA_RELEASE} has no images.txt with noma-server and noma-workspace" >&2
    exit 1
  fi
  CLAUDE=real
else
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
fi

kc apply -f "${ROOT}/deploy/dev/rbac.yaml" -f "${ROOT}/deploy/dev/server.yaml"
# Real Claude needs room for the Remote Control server and its sessions; the
# fake one needs almost nothing, so tests can run several workspaces.
if [ "$CLAUDE" = real ]; then memory=2Gi; else memory=256Mi; fi
kc -n noma-system set env deployment/noma-server NOMA_WORKSPACE_MEMORY="$memory" >/dev/null
if [ -n "${NOMA_RELEASE:-}" ]; then
  kc -n noma-system patch deployment/noma-server --type=json -p "[
    {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/image\",\"value\":\"${server_image}\"},
    {\"op\":\"replace\",\"path\":\"/spec/template/spec/containers/0/imagePullPolicy\",\"value\":\"IfNotPresent\"}]" >/dev/null
  kc -n noma-system set env deployment/noma-server NOMA_WORKSPACE_IMAGE="$workspace_image" >/dev/null
fi
kc -n noma-system rollout restart deployment/noma-server
kc -n noma-system rollout status deployment/noma-server --timeout=120s
