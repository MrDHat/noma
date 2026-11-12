#!/usr/bin/env bash
# Builds Noma's images inside the dev cluster and installs the Noma chart,
# and builds bin/noma for this machine. make dev-up runs this; run it again
# (make deploy) after changing the code.
# NOMA_CLAUDE=fake swaps the pinned Claude Code in the workspace image for
# the fake one the e2e tests use. NOMA_RELEASE=vX.Y.Z deploys that release's
# published images instead of building them (make daily-up).
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
CLAUDE="${NOMA_CLAUDE:-real}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
kc() { kubectl --context "$PROFILE" "$@"; }

arch="$(minikube -p "$PROFILE" ssh -- uname -m | tr -d '\r')"
case "$arch" in
  aarch64) goarch=arm64 ;;
  x86_64) goarch=amd64 ;;
  *) echo "unsupported node architecture: $arch" >&2; exit 1 ;;
esac

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
  # The noma CLI for this machine, then the binaries for the images.
  make -C "$ROOT" build BINARIES=noma
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
  if [ "$CLAUDE" = fake ]; then
    # Stand-ins for a repo's own image (noma.yaml), for the e2e tests.
    image noma-e2e-project "${ROOT}/build/e2e-project.Dockerfile"
    image noma-e2e-nogit "${ROOT}/build/e2e-project.Dockerfile" -- --build-opt="opt=build-arg:GIT=no"
  fi
fi

# Real Claude needs room for the Remote Control server and its sessions; the
# fake one needs almost nothing, so tests can run several workspaces.
values=(-f "${ROOT}/charts/noma/values-minikube.yaml")
if [ "$CLAUDE" = real ]; then
  values+=(--set workspace.memory=2Gi --set workspace.fakeClaude=false)
else
  values+=(--set workspace.memory=256Mi --set workspace.fakeClaude=true)
fi
if [ -n "${NOMA_RELEASE:-}" ]; then
  values+=(--set "server.image=${server_image}" --set server.pullPolicy=IfNotPresent
    --set "workspace.image=${workspace_image}")
fi

# Clusters made before the chart (M3.5-14) have the same objects, applied
# with kubectl. Mark them as the release's so Helm takes them over, keeping
# Postgres's data and the secrets.
if ! helm --kube-context "$PROFILE" -n noma-system status noma >/dev/null 2>&1; then
  for obj in secret/postgres secret/noma-server secret/noma-secret-key secret/noma-server-tls \
    service/postgres statefulset/postgres serviceaccount/noma-server service/noma-server \
    deployment/noma-server clusterrole/noma-server clusterrolebinding/noma-server; do
    if kc -n noma-system get "$obj" >/dev/null 2>&1; then
      kc -n noma-system label --overwrite "$obj" app.kubernetes.io/managed-by=Helm >/dev/null
      kc -n noma-system annotate --overwrite "$obj" meta.helm.sh/release-name=noma \
        meta.helm.sh/release-namespace=noma-system >/dev/null
    fi
  done
fi

# --force-conflicts: the chart owns these objects, including fields kubectl
# set on clusters made before it.
helm --kube-context "$PROFILE" upgrade --install noma "${ROOT}/charts/noma" \
  -n noma-system --create-namespace "${values[@]}" --force-conflicts --wait --timeout 5m
if [ -z "${NOMA_RELEASE:-}" ]; then
  # The images keep their :dev tag, so a new build needs a restart.
  kc -n noma-system rollout restart deployment/noma-server
  kc -n noma-system rollout status deployment/noma-server --timeout=120s
fi
