# Storage for workspaces (M3.5-20, M3.5-23): EBS gp3 volumes for each
# workspace, and EFS for each user's Claude login, which every workspace of
# that user mounts from any node. The CSI drivers get their AWS access
# through EKS Pod Identity, not the node role.

resource "aws_eks_addon" "pod_identity" {
  cluster_name = aws_eks_cluster.main.name
  addon_name   = "eks-pod-identity-agent"
}

# A role that one Kubernetes ServiceAccount assumes through Pod Identity.
resource "aws_iam_role" "csi" {
  for_each = local.csi_drivers
  name     = "${var.cluster_name}-${each.key}"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "pods.eks.amazonaws.com" }
      Action    = ["sts:AssumeRole", "sts:TagSession"]
    }]
  })
}

locals {
  csi_drivers = {
    ebs-csi = {
      addon  = "aws-ebs-csi-driver"
      sa     = "ebs-csi-controller-sa"
      policy = "arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy"
      # Volumes carry the cluster's tag, so make aws-down can find any
      # the cluster leaves behind (M3.5-21).
      config = { controller = { extraVolumeTags = { "noma:cluster" = var.cluster_name } } }
    }
    efs-csi = {
      addon  = "aws-efs-csi-driver"
      sa     = "efs-csi-controller-sa"
      policy = "arn:aws:iam::aws:policy/service-role/AmazonEFSCSIDriverPolicy"
      config = {}
    }
  }
}

resource "aws_iam_role_policy_attachment" "csi" {
  for_each   = local.csi_drivers
  role       = aws_iam_role.csi[each.key].name
  policy_arn = each.value.policy
}

resource "aws_eks_pod_identity_association" "csi" {
  for_each        = local.csi_drivers
  cluster_name    = aws_eks_cluster.main.name
  namespace       = "kube-system"
  service_account = each.value.sa
  role_arn        = aws_iam_role.csi[each.key].arn
}

resource "aws_eks_addon" "csi" {
  for_each             = local.csi_drivers
  cluster_name         = aws_eks_cluster.main.name
  addon_name           = each.value.addon
  configuration_values = length(each.value.config) > 0 ? jsonencode(each.value.config) : null
  depends_on = [
    aws_eks_addon.pod_identity,
    aws_eks_pod_identity_association.csi,
    aws_eks_node_group.main,
  ]
}

# The login file system. Each user's login claim is an access point on it.
resource "aws_efs_file_system" "login" {
  encrypted = true
  tags = {
    Name = "${var.cluster_name}-login"
  }
}

# NFS from the cluster's own security group, which the nodes are in, and
# nothing else.
resource "aws_security_group" "efs" {
  name        = "${var.cluster_name}-efs"
  description = "NFS to the login file system from the cluster's nodes"
  vpc_id      = aws_vpc.main.id
}

resource "aws_vpc_security_group_ingress_rule" "efs_nfs" {
  security_group_id            = aws_security_group.efs.id
  description                  = "NFS from the cluster's nodes"
  ip_protocol                  = "tcp"
  from_port                    = 2049
  to_port                      = 2049
  referenced_security_group_id = aws_eks_cluster.main.vpc_config[0].cluster_security_group_id
}

resource "aws_efs_mount_target" "login" {
  count           = length(aws_subnet.public)
  file_system_id  = aws_efs_file_system.login.id
  subnet_id       = aws_subnet.public[count.index].id
  security_groups = [aws_security_group.efs.id]
}

output "login_file_system" {
  value = aws_efs_file_system.login.id
}
