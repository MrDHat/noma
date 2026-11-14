# Noma on AWS

Terraform for the cluster Noma runs on: a VPC, EKS, and (next) a Graviton
node group with gVisor. Installing Noma itself is the Helm chart's job.

Once per AWS account, create the state bucket:

```bash
terraform -chdir=infra/aws/bootstrap init
terraform -chdir=infra/aws/bootstrap apply
```

Then the cluster, with the bucket the bootstrap printed:

```bash
terraform -chdir=infra/aws init -backend-config="bucket=noma-tfstate-<account id>" -backend-config="region=us-east-1"
terraform -chdir=infra/aws apply
```

Variables: `region`, `profile`, `cluster_name`, `kubernetes_version`.
