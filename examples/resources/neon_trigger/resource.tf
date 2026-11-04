resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

# schedule trigger: invokes the Function on a cron schedule.
# function_slug is a free-form string; pair with neon_function via depends_on if
# you need Terraform to manage ordering.
resource "neon_trigger" "schedule" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.branch.id
  name          = "every-five-minutes"
  type          = "schedule"
  function_slug = "my-handler"
  schedule = {
    cron = "*/5 * * * *"
  }
}

# storage_object_created trigger: invokes the Function when an object is
# uploaded to the bucket. Requires neon_bucket to exist.
resource "neon_bucket" "example" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  name       = "uploads"
}

resource "neon_trigger" "storage" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.branch.id
  name          = "on-upload"
  type          = "storage_object_created"
  function_slug = "my-handler"
  storage_object_created = {
    bucket_name = neon_bucket.example.name
    prefix      = "incoming/"
  }
  depends_on = [neon_bucket.example]
}
