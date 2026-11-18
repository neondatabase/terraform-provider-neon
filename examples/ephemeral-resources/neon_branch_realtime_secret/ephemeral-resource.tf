resource "neon_project" "realtime_secret" {
  name      = "realtime-secret-example"
  region_id = "aws-us-east-2"

  # Realtime enables logical replication on the project.
  enable_logical_replication = true
}

resource "neon_branch_realtime" "secret_example" {
  project_id = neon_project.realtime_secret.id
  branch_id  = neon_project.realtime_secret.default_branch_id
}

# Pass the secret to a write-only argument so it never reaches the state.
ephemeral "neon_branch_realtime_secret" "this" {
  project_id = neon_branch_realtime.secret_example.project_id
  branch_id  = neon_branch_realtime.secret_example.branch_id
}

resource "aws_ssm_parameter" "realtime_secret" {
  name             = "/neon/realtime/${neon_branch_realtime.secret_example.id}"
  type             = "SecureString"
  value_wo         = ephemeral.neon_branch_realtime_secret.this.secret
  value_wo_version = 1
}
