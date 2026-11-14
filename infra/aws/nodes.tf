# Graviton nodes that run workspaces under gVisor (M3.5-4, M3.5-6, M3.5-7).
# Each node installs the pinned gVisor release at boot, checks its SHA-512,
# and registers runsc with containerd. The cluster's gvisor RuntimeClass
# (make aws-up) points at it.

locals {
  # The same release and checksum as hack/dev-up.sh.
  gvisor_release = "20261026.0"
  gvisor_sha512  = "da177d9bcfd9ac23899ecfedc6fd47358e9684a1f7e3f5f8ffb8bf7c0ca65f4c08730c0c503b9231b522db28b04c132a06d04d6719e0d8c04c9f82edaf03d8ae"
}

resource "aws_iam_role" "node" {
  name = "${var.cluster_name}-node"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "node" {
  for_each = toset([
    "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
    "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
  ])
  role       = aws_iam_role.node.name
  policy_arn = each.value
}

resource "aws_launch_template" "node" {
  name_prefix = "${var.cluster_name}-node-"

  # IMDSv2 only, and one hop, so pods can't reach the node's credentials
  # (M3.5-22).
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = var.node_disk_size
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }

  # AL2023 runs this before nodeadm starts containerd. containerd reads
  # /etc/containerd/config.d, which nodeadm's config imports.
  user_data = base64encode(<<-EOT
    MIME-Version: 1.0
    Content-Type: multipart/mixed; boundary="//"

    --//
    Content-Type: text/x-shellscript; charset="us-ascii"

    #!/bin/bash
    set -euo pipefail
    dir="$(mktemp -d)"
    curl -fsSL -o "$dir/gvisor.tar.bz2" \
      "https://storage.googleapis.com/gvisor/releases/release/${local.gvisor_release}/aarch64/gvisor.tar.bz2"
    echo "${local.gvisor_sha512}  $dir/gvisor.tar.bz2" | sha512sum -c -
    tar -xjf "$dir/gvisor.tar.bz2" -C "$dir"
    install -m 755 "$dir/runsc" "$dir/containerd-shim-runsc-v1" /usr/local/bin/
    mkdir -p /usr/local/bin/gvisor-bin /etc/containerd/config.d
    install -m 755 "$dir"/gvisor-bin/* /usr/local/bin/gvisor-bin/
    cat >/etc/containerd/config.d/runsc.toml <<'TOML'
    version = 2
    [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
      runtime_type = "io.containerd.runsc.v1"
    TOML
    rm -rf "$dir"

    --//--
    EOT
  )

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.cluster_name}-node"
    }
  }
}

resource "aws_eks_node_group" "main" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "${var.cluster_name}-arm64"
  node_role_arn   = aws_iam_role.node.arn
  subnet_ids      = aws_subnet.public[*].id
  ami_type        = "AL2023_ARM_64_STANDARD"
  instance_types  = [var.node_instance_type]

  scaling_config {
    desired_size = var.node_count
    min_size     = var.node_count
    max_size     = var.node_count
  }

  launch_template {
    id      = aws_launch_template.node.id
    version = aws_launch_template.node.latest_version
  }

  update_config {
    max_unavailable = 1
  }

  depends_on = [aws_iam_role_policy_attachment.node, aws_eks_addon.vpc_cni]
}
