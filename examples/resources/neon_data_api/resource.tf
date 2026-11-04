resource "neon_project" "this" {
  name      = "data-api-example"
  region_id = "aws-us-east-2"
}

resource "neon_data_api" "this" {
  project_id    = neon_project.this.id
  branch_id     = neon_project.this.branch.id
  database_name = neon_project.this.database_name

  auth_provider = "neon_auth"

  settings = {
    db_schemas  = ["public"]
    db_max_rows = 100
  }
}

output "data_api_url" {
  value = neon_data_api.this.url
}
