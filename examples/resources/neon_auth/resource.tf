variable "smtp_password" {
  type      = string
  sensitive = true
}

resource "neon_project" "example" {
  name      = "app"
  region_id = "aws-us-east-2"
}

resource "neon_auth" "example" {
  project_id      = neon_project.example.id
  branch_id       = neon_project.example.branch.id
  allow_localhost = false

  email_provider = {
    type         = "standard"
    host         = "smtp.resend.com"
    port         = 587
    username     = "resend"
    password     = var.smtp_password
    sender_email = "auth@example.com"
    sender_name  = "Example"
  }
  email_and_password = {
    require_email_verification = true
    email_verification_method  = "link"
  }
  magic_link = {
    enabled    = true
    expires_in = 15
  }
  organization = {
    enabled      = true
    creator_role = "admin"
  }
  webhook = {
    enabled        = true
    url            = "https://example.com/neon-auth"
    enabled_events = ["user.created"]
  }

  # Neon cannot enable Neon Auth again on this branch once it is disabled.
  lifecycle {
    prevent_destroy = true
  }
}

output "neon_auth_base_url" {
  value = neon_auth.example.base_url
}
