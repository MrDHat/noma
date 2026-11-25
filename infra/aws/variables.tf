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
