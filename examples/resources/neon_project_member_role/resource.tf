resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

resource "neon_project_member_role" "example" {
  project_id = neon_project.example.id
  member_id  = "28012f1d-fe85-4d69-be44-79f561ce10f6"
  role       = "editor"
}
