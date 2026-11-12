terraform {
  required_version = ">= 1.10"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.70"
    }
  }
  # Partial config: make aws-up passes the bucket from infra/aws/bootstrap.
  # use_lockfile is Terraform's native S3 lock, with no DynamoDB table.
  backend "s3" {
    key          = "noma/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  region  = var.region
  profile = var.profile
  default_tags {
    tags = {
      "noma:cluster" = var.cluster_name
    }
  }
}
