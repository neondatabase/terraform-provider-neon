resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

# the bucket is private by default
resource "neon_bucket" "private" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  name       = "foo"
}

# set the access level to allow for public read operations
resource "neon_bucket" "public" {
  project_id   = neon_project.example.id
  branch_id    = neon_project.example.branch.id
  name         = "bar"
  access_level = "public_read"
}
