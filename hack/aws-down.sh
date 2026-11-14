#!/usr/bin/env bash
# Takes down what make aws-up made and leaves nothing that bills (M3.5-18,
# verify/teardown):
#   1. helm uninstall --wait. The chart's guard fails it while any
#      workspace exists, and noma-server is gone before the next step.
#   2. Delete the user namespaces and noma-system, which take every
#      workspace, login and Postgres claim, and wait for their volumes to go.
#      If they don't, stop with an error and keep the cluster.
#   3. terraform destroy.
#   4. Fail if any EBS volume tagged with the cluster is left.
#
#   make aws-down               # AWS_PROFILE and AWS_REGION pick the account
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE="${AWS_PROFILE:-default}"
REGION="${AWS_REGION:-us-east-1}"
CONTEXT="${NOMA_AWS_CONTEXT:-noma-aws}"
WAIT="${NOMA_VOLUME_WAIT:-600}"
tf() { terraform -chdir="${ROOT}/infra/aws" "$@"; }
k() { kubectl --context "$CONTEXT" "$@"; }

account="$(aws sts get-caller-identity --profile "$PROFILE" --query Account --output text)"
tf init -input=false -backend-config="bucket=noma-tfstate-${account}" -backend-config="region=${REGION}" \
  -backend-config="profile=${PROFILE}" >/dev/null
cluster="$(tf output -raw cluster_name 2>/dev/null || true)"

if [ -n "$cluster" ] && aws eks describe-cluster --profile "$PROFILE" --region "$REGION" --name "$cluster" >/dev/null 2>&1; then
  aws eks update-kubeconfig --profile "$PROFILE" --region "$REGION" --name "$cluster" --alias "$CONTEXT" >/dev/null

  if helm --kube-context "$CONTEXT" -n noma-system status noma >/dev/null 2>&1; then
    echo "==> uninstall Noma"
    if ! helm --kube-context "$CONTEXT" -n noma-system uninstall noma --wait --timeout 5m; then
      echo "helm uninstall failed. If workspaces still exist, remove them with noma rm and run make aws-down again." >&2
      k -n noma-system logs job/noma-uninstall-guard >&2 2>/dev/null || true
      exit 1
    fi
  fi

  echo "==> delete Noma's namespaces and volumes"
  namespaces="$(k get namespaces -l noma.dev/managed=true -o name) $(k get namespace noma-system -o name --ignore-not-found)"
  # shellcheck disable=SC2086 # one word per namespace
  [ -z "${namespaces// }" ] || k delete $namespaces --wait=false
  deadline=$((SECONDS + WAIT))
  until [ -z "$(k get namespaces -l noma.dev/managed=true -o name) $(k get namespace noma-system -o name --ignore-not-found)" ] \
    && [ -z "$(k get pv -o name)" ]; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "Noma's namespaces or volumes are still there after ${WAIT}s; the cluster is kept so nothing is orphaned:" >&2
      k get namespaces -l noma.dev/managed=true >&2 || true
      k get pv >&2 || true
      exit 1
    fi
    sleep 5
  done
fi

echo "==> destroy the cluster"
tf destroy -input=false -auto-approve -var "profile=${PROFILE}" -var "region=${REGION}"

if [ -n "$cluster" ]; then
  left="$(aws ec2 describe-volumes --profile "$PROFILE" --region "$REGION" \
    --filters "Name=tag:noma:cluster,Values=${cluster}" --query 'Volumes[].VolumeId' --output text)"
  if [ -n "$left" ]; then
    echo "EBS volumes tagged noma:cluster=${cluster} are left, and still bill: ${left}" >&2
    exit 1
  fi
fi
echo "Everything make aws-up made is gone. The Terraform state bucket stays (noma-tfstate-${account})."
