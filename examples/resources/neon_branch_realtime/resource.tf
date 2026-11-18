resource "neon_project" "realtime" {
  name      = "realtime-example"
  region_id = "aws-us-east-2"

  # Realtime enables logical replication on the project.
  enable_logical_replication = true
}

resource "neon_branch_realtime" "this" {
  project_id      = neon_project.realtime.id
  branch_id       = neon_project.realtime.default_branch_id
  allowed_origins = ["https://app.example.com"]
}

output "realtime_url" {
  value = neon_branch_realtime.this.invocation_url
}
