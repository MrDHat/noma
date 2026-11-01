#!/usr/bin/env bash
# Deletes the local Noma cluster and everything in it.
set -euo pipefail
minikube -p "${NOMA_PROFILE:-noma}" delete
