resource "neon_branch_auth" "app" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.default_branch_id
  database_name = neon_project.example.database_name
}

output "auth" {
  value = {
    base_url = neon_branch_auth.app.base_url
    jwks_url = neon_branch_auth.app.jwks_url
  }
}
