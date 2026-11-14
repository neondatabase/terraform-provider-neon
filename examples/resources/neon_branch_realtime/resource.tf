resource "neon_project" "x" {
  name = "realtime-example"
}

resource "neon_branch_realtime" "this" {
  project_id      = neon_project.x.id
  branch_id       = neon_project.x.branch.id
  allowed_origins = ["https://app.example.com"]
  # For any other branch, substitute neon_branch.example.id above.
  # Omit allowed_origins to preserve current/inherited policy without managing it.
  # [] and ["*"] both explicitly allow any origin.
}
