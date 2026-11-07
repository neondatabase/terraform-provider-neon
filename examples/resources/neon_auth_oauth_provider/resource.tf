resource "neon_auth" "example" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
}

resource "neon_auth_oauth_provider" "github" {
  project_id    = neon_auth.example.project_id
  branch_id     = neon_auth.example.branch_id
  provider_id   = "github"
  client_id     = var.github_client_id
  client_secret = var.github_client_secret
}

resource "neon_auth_oauth_provider" "apple" {
  project_id    = neon_auth.example.project_id
  branch_id     = neon_auth.example.branch_id
  provider_id   = "apple"
  client_id     = var.apple_services_id
  client_secret = file("${path.module}/AuthKey.p8")
  apple_key_id  = var.apple_key_id
  apple_team_id = var.apple_team_id
}
