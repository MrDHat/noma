#!/usr/bin/env bash
# Model-checks one TLA+ model under verify/ with TLC. verify/<model>/checks
# lists each check and what it should do: "pass", or "fail <Property>" for a
# known finding that the code still has. The script fails when any check
# does something else, so a fixed finding shows up as well as a new bug.
#
#   hack/verify-model.sh lifecycle            run every lifecycle check
#   hack/verify-model.sh lifecycle rollout    run the checks whose name matches
#   VERBOSE=1 hack/verify-model.sh credentials   TLC's whole output, with states
set -euo pipefail
cd "$(dirname "$0")/.."

TLA_VERSION=1.7.4
TLA_SHA256=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
JAR=${TLA2TOOLS:-.dev/tla2tools-$TLA_VERSION.jar}

[[ $# -ge 1 && -f verify/${1:-}/checks ]] || { echo "usage: $0 <model under verify/> [check]" >&2; exit 2; }
dir=verify/$1
filter=${2:-}
spec=$(cd "$dir" && ls ./*.tla | head -1)

command -v java >/dev/null || { echo "verify-model: needs Java 11 or later on PATH" >&2; exit 1; }
if [[ ! -f $JAR ]]; then
  mkdir -p "$(dirname "$JAR")"
  curl -fsSL -o "$JAR.tmp" "https://github.com/tlaplus/tlaplus/releases/download/v$TLA_VERSION/tla2tools.jar"
  echo "$TLA_SHA256  $JAR.tmp" | shasum -a 256 -c - >/dev/null
  mv "$JAR.tmp" "$JAR"
fi
case $JAR in /*) ;; *) JAR=$PWD/$JAR ;; esac

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
status=0
while IFS='|' read -r name cfg expected; do
  [[ -z $name || $name == \#* ]] && continue
  [[ -n $filter && $name != *"$filter"* ]] && continue
  log="$out/$name.log"
  (cd "$dir" && java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC \
    -config "$cfg" -workers auto -metadir "$out/$name" -cleanup "$spec") >"$log" 2>&1 || true

  prop=$(sed -nE 's/^Error: (Invariant|Action property) ([A-Za-z]+) is violated.*/\2/p' "$log" | head -1)
  if grep -q "^Error: Temporal properties were violated" "$log"; then
    # TLC does not name the temporal property; the config lists them.
    prop=$(sed -nE 's/^PROPERTIES +//p' "$dir/$cfg" | head -1)
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
done <"$dir/checks"
exit $status
