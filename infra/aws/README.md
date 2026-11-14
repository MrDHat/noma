# Noma on AWS

Terraform for the cluster Noma runs on: a VPC, EKS, a Graviton node group
that installs gVisor at boot, and the EBS and EFS storage workspaces use. Installing Noma itself is the Helm chart's job.

`make aws-up` does all of the below with your `AWS_PROFILE` and `AWS_REGION`,
then creates the `gvisor` RuntimeClass and the `noma-workspace` (EBS) and
`noma-login` (EFS) StorageClasses, and prints the `helm install` and
`noma connect` commands. By hand:

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

Variables: `region`, `profile`, `cluster_name`, `kubernetes_version`,
`node_instance_type` (t4g.large), `node_count` (1), `node_disk_size` (50 GiB).
