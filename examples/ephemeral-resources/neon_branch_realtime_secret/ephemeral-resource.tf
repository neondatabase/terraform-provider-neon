terraform {
  required_version = ">= 1.10"
}

resource "neon_project" "x" {
  name = "realtime-secret-example"
}

resource "neon_branch_realtime" "this" {
  project_id      = neon_project.x.id
  branch_id       = neon_project.x.branch.id
  allowed_origins = ["https://app.example.com"]
}

ephemeral "neon_branch_realtime_secret" "this" {
  project_id = neon_project.x.id
  branch_id  = neon_project.x.branch.id
  depends_on = [neon_branch_realtime.this]
}

# Pass ephemeral.neon_branch_realtime_secret.this.secret only to an
# ephemeral-capable consumer (for example a write-only provider argument).
# Do not write it to an ordinary output, local file, or persistent resource field.
# pending=true may accompany the previous usable secret during rotation.
