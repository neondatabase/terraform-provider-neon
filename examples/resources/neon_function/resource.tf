resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

# The ZIP must contain an index.mjs at the root whose default export has a fetch handler.
# In production, build the ZIP in your CI pipeline.
data "archive_file" "function" {
  type        = "zip"
  output_path = "${path.module}/function.zip"

  source {
    filename = "index.mjs"
    content  = <<-EOT
      export default {
        fetch: () => new Response("Hello world!"),
      };
    EOT
  }
}

resource "neon_function" "example" {
  project_id    = neon_project.example.id
  branch_id     = neon_project.example.branch.id
  slug          = "hello"
  runtime       = "nodejs24"
  name          = "hello"
  zip_file_path = data.archive_file.function.output_path

  # Redeploys when the code changes, even though the ZIP path stays the same.
  source_code_hash = data.archive_file.function.output_base64sha256

  environment_variables = {
    LOG_LEVEL = "info"
  }
}
