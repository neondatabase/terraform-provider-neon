resource "neon_project" "example" {
  name      = "app"
  region_id = "aws-us-east-2"
}

resource "neon_auth" "example" {
  project_id      = neon_project.example.id
  branch_id       = neon_project.example.branch.id
  allow_localhost = false

  # Neon cannot enable Neon Auth again on this branch once it is disabled.
  lifecycle {
    prevent_destroy = true
  }
}

output "neon_auth_base_url" {
  value = neon_auth.example.base_url
}
