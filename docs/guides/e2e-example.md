---
page_title: "End-to-end example: AWS Application"
---

# End-to-end example: AWS Application

The guide illustrates how to provision a Neon project with a dedicated database, role and compute resources as well as
AWS Secret which contains access credentials.

## Prerequisites

- Terraform 1.11 or later, for the ephemeral `neon_role` resource and the write-only `secret_string_wo` argument
- A Neon [API key](https://neon.com/docs/manage/api-keys)
- AWS account and role with sufficient privileges

## Code Snippet

The code below documents the following resources:

- the Neon stack:
    - project;
    - dedicated branch with
        - role;
        - database;
- the AWS Secretsmanager secret to store database access details securely;
- the AWS IAM policy to be attached to the AWS IAM role assumed by an application intended to communicate with the Neon
  database.

```terraform
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
```

## Execution

1. Export API keys as environment variables:
   ```commandline
   export NEON_API_KEY=##neon api key##
   export AWS_ACCESS_KEY_ID=##AWS access key ID##
   export AWS_SECRET_ACCESS_KEY=##AWS access secret##
   ```
2. Create the file `main.tf` and copy the code [snippet](#code-snippet) there.
3. Initialize the terraform to download required providers:
   ```commandline
   terraform init
   ```
   ~> Note that the terraform state will be stored locally. Find more
   details [here](https://developer.hashicorp.com/terraform/language/settings/backends/configuration).
4. Validate the syntax correctness:
   ```commandline
   terraform validate
   ```
5. Run terraform plan:
   ```commandline
   terraform plan
   ```
   The plan ends with:

    ```commandline
    Plan: 8 to add, 0 to change, 0 to destroy.
    ```

6. Run terraform apply:
   ```commandline
   terraform apply -auto-approve
   ```
   Expected output in stdout:

    ```commandline
    Apply complete! Resources: 8 added, 0 changed, 0 destroyed.
    ```

7. Done! The database can be accessed using the connection details from the AWS secretsmanager.
8. Clean the demo infrastructure:
   ```commandline
   terraform destroy -auto-approve
   ```
   Expected output in stdout:
   ```commandline
   Destroy complete! Resources: 8 destroyed.
   ```
9. Unset environment variables:
   ```commandline
   unset NEON_API_KEY
   unset AWS_ACCESS_KEY_ID
   unset AWS_SECRET_ACCESS_KEY
   ```
