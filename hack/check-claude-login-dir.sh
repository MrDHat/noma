#!/usr/bin/env bash
# Checks that Claude Code still keeps its login where CLAUDE_SECURESTORAGE_CONFIG_DIR
# points. Noma shares one Claude login per user through that setting, and it is
# undocumented (decisions M3-5, M3-9), so a release could drop it at any time.
#
#   hack/check-claude-login-dir.sh pinned   the version build/workspace.Dockerfile pins
#   hack/check-claude-login-dir.sh latest   the newest release on the latest channel
#   hack/check-claude-login-dir.sh 2.1.318  a given version
#
# Needs no Claude account: a dummy credentials file is read offline. Linux only,
# since Claude on macOS keeps its login in the Keychain.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE=https://downloads.claude.ai/claude-code-releases
version=${1:?usage: $0 pinned|latest|VERSION}
case $version in
pinned) version=$(sed -nE 's/^ARG CLAUDE_VERSION=//p' build/workspace.Dockerfile) ;;
latest) version=$(curl -fsSL "$BASE/latest") ;;
esac
case "$(uname -s)-$(uname -m)" in
Linux-x86_64) platform=linux-x64 ;;
Linux-aarch64) platform=linux-arm64 ;;
*) echo "check-claude-login-dir: runs on Linux only" >&2; exit 2 ;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
sum=$(curl -fsSL "$BASE/$version/manifest.json" | jq -er --arg p "$platform" '.platforms[$p].checksum')
curl -fsSLo "$work/claude" "$BASE/$version/$platform/claude"
echo "$sum  $work/claude" | sha256sum -c --quiet -
chmod +x "$work/claude"

# A login that only exists in the shared directory. It is never sent anywhere:
# auth status reads it without calling Anthropic.
mkdir -p "$work/home" "$work/shared"
cat >"$work/shared/.credentials.json" <<'JSON'
{"claudeAiOauth":{"accessToken":"sk-ant-oat01-noma-ci-dummy","refreshToken":"sk-ant-ort01-noma-ci-dummy","expiresAt":4102444800000,"scopes":["user:inference","user:profile"],"subscriptionType":"pro"}}
JSON

# loggedIn as auth status reports it; it exits 1 when logged out.
logged_in() {
  env -i PATH="$PATH" HOME="$work/home" DISABLE_AUTOUPDATER=1 "$@" \
    "$work/claude" auth status 2>/dev/null | jq -r '.loggedIn' || true
}

failed=0
check() {
  if [[ $2 == "$3" ]]; then echo "ok    $1"; else echo "FAIL  $1: got '$2', want '$3'"; failed=1; fi
}
check "without the setting, Claude does not find the login" "$(logged_in)" false
check "with the setting, Claude reads the login from it" \
  "$(logged_in CLAUDE_SECURESTORAGE_CONFIG_DIR="$work/shared")" true
check "no credentials were written under HOME" \
  "$(find "$work/home" -name '.credentials.json' | wc -l | tr -d ' ')" 0

if [[ $failed == 1 ]]; then
  echo "Claude $version no longer honours CLAUDE_SECURESTORAGE_CONFIG_DIR; do not bump the pin to it." >&2
  exit 1
fi
echo "Claude $version keeps its login in CLAUDE_SECURESTORAGE_CONFIG_DIR"
