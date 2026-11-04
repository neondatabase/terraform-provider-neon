resource "neon_project" "example" {
  name = "example"
}

# store the default role's password to the AWS SSM Parameters Store instead of the terraform state
# using ephemeral resource
ephemeral "neon_role" "default" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  name       = neon_project.example.database_user
  lifecycle {
    postcondition {
      condition     = self.password != ""
      error_message = "Empty role password returned"
    }
  }
}

locals {
  secret_name = "${neon_project.example.id}/${neon_project.example.branch.id}/${neon_project.example.database_user}"
}

resource "aws_ssm_parameter" "default_neon_role_password" {
  name             = "/neonRole/${local.secret_name}"
  type             = "SecureString"
  value_wo         = ephemeral.neon_role.default.password
  value_wo_version = 1
}
