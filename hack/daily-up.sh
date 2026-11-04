#!/usr/bin/env bash
# Brings up the noma-daily cluster from a published release (M3-24): the
# same setup as make dev-up, on its own minikube profile, with the release's
# images instead of ones built from this checkout. A broken branch or
# make dev-up then can't take down the workspace being used to write it.
#
#   make daily-up VERSION=v0.3.0
#   NOMA_CONFIG=~/.config/noma/daily.json noma ls
set -euo pipefail
VERSION="${1:?usage: $0 VERSION (a release tag, like v0.3.0)}"
export NOMA_PROFILE=noma-daily
export NOMA_RELEASE="$VERSION"
export NOMA_CONFIG="${HOME}/.config/noma/daily.json"
exec "$(dirname "$0")/dev-up.sh"
