variable "region" {
  type    = string
  default = "us-east-1"
}

variable "profile" {
  description = "The AWS CLI profile Terraform uses."
  type        = string
  default     = "default"
}

variable "cluster_name" {
  type    = string
  default = "noma"
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version; minikube and CI run the same one."
  type        = string
  default     = "1.35"
}

variable "vpc_cidr" {
  type    = string
  default = "10.42.0.0/16"
}

variable "node_instance_type" {
  description = "Graviton (arm64) instance type of the workspace nodes."
  type        = string
  default     = "t4g.large"
}

variable "node_count" {
  type    = number
  default = 1
}

variable "node_disk_size" {
  description = "Each node's root volume in GiB: images and container layers."
  type        = number
  default     = 50
}
