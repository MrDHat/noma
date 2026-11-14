#!/usr/bin/env bash
# Publishes a Noma release (M3-24). CI's release workflow runs it on a v* tag:
#
#   hack/release.sh images ARCH   build and push this architecture's images
#   hack/release.sh publish       join them into multi-arch images, push the
#                                 chart, build the CLI, and create the
#                                 GitHub release
#
# The chart goes to oci://ghcr.io/OWNER/charts/noma with the release's
# images by digest as its defaults (M3.5-14); make daily-up installs it.
# The release also carries images.txt, each image by digest. Needs VERSION, OWNER, ACTOR, GH_TOKEN and, for
# publish, REPO.
set -euo pipefail
cd "$(dirname "$0")/.."

: "${VERSION:?}" "${OWNER:?}" "${ACTOR:?}" "${GH_TOKEN:?}"
registry="ghcr.io/${OWNER,,}"
images=(noma-server noma-workspace noma-dev)

echo "$GH_TOKEN" | docker login ghcr.io -u "$ACTOR" --password-stdin >/dev/null

case ${1:-} in
images)
  arch="${2:?usage: $0 images ARCH}"
  out="bin/linux-${arch}"
  make build GOOS=linux GOARCH="$arch" BIN="$out" VERSION="$VERSION"
  build() { # build NAME DOCKERFILE [FILE...] [-- BUILD-ARG...]
    local name="$1" file="$2" ctx tag
    ctx="$(mktemp -d)"
    shift 2
    while [ $# -gt 0 ] && [ "$1" != "--" ]; do cp "$1" "$ctx/"; shift; done
    [ $# -gt 0 ] && shift
    tag="${registry}/${name}:${VERSION}-${arch}"
    docker build --platform "linux/${arch}" -f "$file" -t "$tag" "$@" "$ctx"
    docker push "$tag"
    rm -rf "$ctx"
  }
  build noma-server build/server.Dockerfile "${out}/noma-server"
  build noma-workspace build/workspace.Dockerfile "${out}/noma-agent" -- --build-arg CLAUDE=real
  build noma-dev build/dev.Dockerfile
  ;;
publish)
  : "${REPO:?}"
  rm -rf dist && mkdir dist
  for name in "${images[@]}"; do
    docker buildx imagetools create -t "${registry}/${name}:${VERSION}" \
      "${registry}/${name}:${VERSION}-amd64" "${registry}/${name}:${VERSION}-arm64"
    digest="$(docker buildx imagetools inspect "${registry}/${name}:${VERSION}" --format '{{json .Manifest}}' | jq -r .digest)"
    echo "${name}=${registry}/${name}@${digest}" >>dist/images.txt
  done
  # The chart, defaulting to this release's images.
  chart="$(mktemp -d)"
  cp -R charts/noma "$chart/"
  server="$(sed -n 's/^noma-server=//p' dist/images.txt)"
  workspace="$(sed -n 's/^noma-workspace=//p' dist/images.txt)"
  sed -i -e "s#^  image: noma-server:dev\$#  image: ${server}#" \
    -e "s#^  image: noma-workspace:dev\$#  image: ${workspace}#" "$chart/noma/values.yaml"
  grep -q "image: ${server}" "$chart/noma/values.yaml"
  grep -q "image: ${workspace}" "$chart/noma/values.yaml"
  helm package "$chart/noma" --version "${VERSION#v}" --app-version "$VERSION" -d "$chart"
  echo "$GH_TOKEN" | helm registry login ghcr.io -u "$ACTOR" --password-stdin
  helm push "$chart/noma-${VERSION#v}.tgz" "oci://${registry}/charts"
  rm -rf "$chart"
  for os in darwin linux; do
    for arch in amd64 arm64; do
      CGO_ENABLED="0" GOOS="$os" GOARCH="$arch" go build -trimpath \
        -ldflags "-s -w -X github.com/MrDHat/noma/internal/version.Version=${VERSION}" \
        -o "dist/noma-${os}-${arch}" ./cmd/noma
    done
  done
  (cd dist && sha256sum -- * >SHA256SUMS)
  gh release create "$VERSION" --repo "$REPO" --title "Noma ${VERSION}" --verify-tag \
    --notes "Install with helm install noma oci://${registry}/charts/noma --version ${VERSION#v}, or run make daily-up VERSION=${VERSION}. Images by digest are in images.txt." dist/*
  ;;
*)
  echo "usage: $0 images ARCH | publish" >&2
  exit 2
  ;;
esac
