resource "neon_project" "example" {
  name = "foo"
}

resource "neon_snapshot" "example" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  name       = "example-snapshot"
  expires_at = "2030-01-01T00:00:00Z"
}
