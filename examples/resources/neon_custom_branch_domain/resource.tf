resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

resource "neon_function" "example" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  name          = "hello"
  zip_file_path = "path/to/your/function/deployment.zip"

  environment_variables = {
    LOG_LEVEL = "info"
  }
}

resource "neon_custom_branch_domain" "example" {
  project_id  = neon_project.example.id
  branch_id   = neon_project.example.branch.id
  entity_type = "function"
  entity_id   = neon_function.example.slug
  domain      = "example.com"
}
