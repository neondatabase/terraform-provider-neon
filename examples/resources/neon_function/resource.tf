resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

# Minimal Node.js function. The zip must contain an index.js handler
# at the root; the Functions service builds it on deploy.
resource "local_file" "function_source" {
  filename = "${path.module}/function.zip"
  content  = templatefile("${path.module}/index.js.tftpl", {})
}

# the file provisioner pattern shown below packages the source into a zip
# at plan time; in production, build the zip in your CI pipeline.
resource "neon_function" "example" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  name          = "hello"
  zip_file_path = local_file.function_source.filename

  environment_variables = {
    LOG_LEVEL = "info"
  }
}
