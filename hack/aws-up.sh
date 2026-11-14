#!/usr/bin/env bash
# Creates the EKS cluster Noma runs on and the prerequisites the chart
# checks for (M3.5-15): the gvisor RuntimeClass and the workspace and login
# StorageClasses. Installing Noma stays the chart's job. Safe to re-run.
#
#   make aws-up                 # AWS_PROFILE and AWS_REGION pick the account
#
# It costs money while it runs: tear it down with make aws-down.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PROFILE="${AWS_PROFILE:-default}"
REGION="${AWS_REGION:-us-east-1}"
CONTEXT="${NOMA_AWS_CONTEXT:-noma-aws}"
tf() { terraform -chdir="${ROOT}/infra/aws" "$@"; }

account="$(aws sts get-caller-identity --profile "$PROFILE" --query Account --output text)"
bucket="noma-tfstate-${account}"
if ! aws s3api head-bucket --profile "$PROFILE" --bucket "$bucket" >/dev/null 2>&1; then
  echo "==> state bucket"
  terraform -chdir="${ROOT}/infra/aws/bootstrap" init -input=false
  terraform -chdir="${ROOT}/infra/aws/bootstrap" apply -input=false \
    -var "profile=${PROFILE}" -var "region=${REGION}"
fi

echo "==> cluster"
tf init -input=false -backend-config="bucket=${bucket}" -backend-config="region=${REGION}" \
  -backend-config="profile=${PROFILE}"
tf apply -input=false -var "profile=${PROFILE}" -var "region=${REGION}"
cluster="$(tf output -raw cluster_name)"
efs="$(tf output -raw login_file_system)"
aws eks update-kubeconfig --profile "$PROFILE" --region "$REGION" --name "$cluster" --alias "$CONTEXT" >/dev/null

echo "==> cluster prerequisites"
kubectl --context "$CONTEXT" apply -f - <<YAML
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
---
# Each workspace's volume: an encrypted gp3 EBS volume in its node's zone.
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: noma-workspace
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
  encrypted: "true"
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
reclaimPolicy: Delete
---
# Each user's Claude login: an EFS access point that every workspace of the
# user mounts, from any node, as the workspace user (uid 1000).
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: noma-login
provisioner: efs.csi.aws.com
parameters:
  provisioningMode: efs-ap
  fileSystemId: ${efs}
  directoryPerms: "700"
  uid: "1000"
  gid: "1000"
reclaimPolicy: Delete
YAML

cat <<EOF2

The cluster is up (kubectl context ${CONTEXT}). Install Noma and point the CLI at it:

  helm upgrade --install noma charts/noma -n noma-system --create-namespace \\
    --set workspace.storageClassName=noma-workspace --set postgres.storageClassName=noma-workspace \\
    --set login.storageClassName=noma-login --set login.accessMode=ReadWriteMany \\
    --set server.image=IMAGE --set workspace.image=IMAGE
  noma connect --context ${CONTEXT}

Tear it down when you are done: make aws-down
EOF2
