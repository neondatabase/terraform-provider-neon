resource "neon_auth" "example" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
}

resource "neon_auth_trusted_domain" "app" {
  project_id = neon_auth.example.project_id
  branch_id  = neon_auth.example.branch_id
  domain     = "https://app.example.com"
}
