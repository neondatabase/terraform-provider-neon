terraform {
  required_version = ">= 1.11"

  required_providers {
    neon = {
      source  = "neondatabase/neon"
      version = "0.19.0-beta.0"
    }

    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.71.0"
    }
  }
}

provider "aws" {
  region = "us-east-2"
}

provider "neon" {}

resource "neon_project" "this" {
  name = "myproject"
}

resource "neon_endpoint" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id

  autoscaling_limit_min_cu = 0.25
  autoscaling_limit_max_cu = 1
  suspend_timeout_seconds  = 300
}

resource "neon_branch" "this" {
  project_id = neon_project.this.id
  parent_id  = neon_project.this.branch.id
  name       = "mybranch"
}

resource "neon_role" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  name       = "myrole"

  # Neon creates roles through the branch's read-write compute.
  depends_on = [neon_endpoint.this]
}

resource "neon_database" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  owner_name = neon_role.this.name
  name       = "mydb"
}

ephemeral "neon_role" "this" {
  project_id = neon_project.this.id
  branch_id  = neon_branch.this.id
  name       = neon_role.this.name
  lifecycle {
    postcondition {
      condition     = self.password != ""
      error_message = "Empty role password returned"
    }
  }
}

resource "aws_secretsmanager_secret" "this" {
  name                    = "neon/mybranch/mydb/myrole"
  description             = "Neon SaaS access details for mydb, myrole @ mybranch"
  recovery_window_in_days = 0

  tags = {
    project  = "demo"
    platform = "neon"
  }
}

resource "aws_secretsmanager_secret_version" "this" {
  secret_id                = aws_secretsmanager_secret.this.id
  secret_string_wo_version = 1
  secret_string_wo = jsonencode({
    user     = ephemeral.neon_role.this.name
    password = ephemeral.neon_role.this.password
    host     = neon_endpoint.this.host
    dbname   = neon_database.this.name
  })
}

data "aws_iam_policy_document" "neon_access_secret" {
  statement {
    effect    = "Allow"
    actions   = ["secretsmanager:ListSecrets"]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "secretsmanager:GetResourcePolicy",
      "secretsmanager:GetSecretValue",
      "secretsmanager:DescribeSecret",
      "secretsmanager:ListSecretVersionIds",
    ]
    resources = [
      aws_secretsmanager_secret_version.this.arn,
    ]
  }
}

resource "aws_iam_policy" "neon_access_secret" {
  name   = "mybranch-mydb-myrole"
  path   = "/neon/read-only/"
  policy = data.aws_iam_policy_document.neon_access_secret.json
}
