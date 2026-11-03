#!/usr/bin/env bash
# Model-checks verify/lifecycle/WorkspaceLifecycle.tla with TLC.
# Each check states what it should do: "pass", or "fail <Property>" for a
# known finding that the code still has. The script fails when any check
# does something else, so a fixed finding shows up as well as a new bug.
#
#   hack/verify-lifecycle.sh            run every check
#   hack/verify-lifecycle.sh rollout    run the checks whose name matches
#   VERBOSE=1 hack/verify-lifecycle.sh  print TLC's whole output, with states
set -euo pipefail
cd "$(dirname "$0")/.."

TLA_VERSION=1.7.4
TLA_SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
JAR=${TLA2TOOLS:-.dev/tla2tools-$TLA_VERSION.jar}

# name | config | expected
CHECKS=(
  "deletion|deletion.cfg|pass"
  "deletion-liveness|deletion-liveness.cfg|pass"
  "agent-phase|agent-phase.cfg|fail AgentPhaseKept"
  "stale-error|stale-error.cfg|fail ErrorClears"
  "rollout|rollout.cfg|pass"
)

command -v java >/dev/null || { echo "verify-lifecycle: needs Java 11 or later on PATH" >&2; exit 1; }
if [[ ! -f $JAR ]]; then
  mkdir -p "$(dirname "$JAR")"
  curl -fsSL -o "$JAR.tmp" "https://github.com/tlaplus/tlaplus/releases/download/v$TLA_VERSION/tla2tools.jar"
  echo "$TLA_SHA256  $JAR.tmp" | shasum -a 256 -c - >/dev/null
  mv "$JAR.tmp" "$JAR"
fi

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
status=0
for check in "${CHECKS[@]}"; do
  IFS='|' read -r name cfg expected <<<"$check"
  [[ $# -gt 0 && $name != *"$1"* ]] && continue
  log="$out/$name.log"
  (cd verify/lifecycle && java -XX:+UseParallelGC -cp "$OLDPWD/$JAR" tlc2.TLC \
    -config "$cfg" -workers auto -metadir "$out/$name" -cleanup WorkspaceLifecycle.tla) >"$log" 2>&1 || true

  prop=$(sed -nE 's/^Error: (Invariant|Action property) ([A-Za-z]+) is violated.*/\2/p' "$log" | head -1)
  if grep -q "^Error: Temporal properties were violated" "$log"; then
    # TLC does not name the temporal property; the config lists them.
    prop=$(sed -nE 's/^PROPERTIES +//p' "verify/lifecycle/$cfg" | head -1)
  fi
  if grep -q "No error has been found" "$log"; then
    got=pass
  elif [[ -n $prop ]]; then
    got="fail $prop"
  else
    got=error
  fi
  if [[ $got == "$expected" ]]; then
    echo "ok    $name: $got"
  else
    echo "FAIL  $name: expected '$expected', got '$got'"
    status=1
  fi
  if [[ -n ${VERBOSE:-} || $got == error ]]; then
    sed 's/^/      /' "$log"
  elif [[ $got != pass ]]; then
    # The counterexample's steps, by action name.
    grep -E '^State [0-9]+: <' "$log" | sed -E 's/^State ([0-9]+): <([A-Za-z]+).*/      \1. \2/'
  fi
done
exit $status
