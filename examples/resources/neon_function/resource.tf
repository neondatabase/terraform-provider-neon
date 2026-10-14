resource "neon_function" "api" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.default_branch_id
  slug       = "api"
  name       = "Application API"
  runtime    = "nodejs24"

  # Build TypeScript/dependencies first; index.mjs must be at the ZIP root.
  zip_file_path    = "${path.module}/api.zip"
  source_code_hash = filebase64sha256("${path.module}/api.zip")

  environment_variables = {
    APP_ENV = "dev"
  }
  deployment_timeout = "10m"
}
