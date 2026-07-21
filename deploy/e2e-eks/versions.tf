terraform {
  required_version = ">= 1.3"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.60"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.2"
    }
  }
}

provider "aws" {
  region = var.region

  # Every resource is disposable — tag it so a forgotten cluster is easy to find
  # in the console (see the mandatory-teardown warning in README.md / ADR-0010).
  default_tags {
    tags = {
      Project   = "kubescope"
      Purpose   = "e2e-test-harness"
      Ephemeral = "true"
    }
  }
}
