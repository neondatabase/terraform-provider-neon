resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

resource "neon_bucket" "example" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  name       = "foo"
}

resource "neon_bucket_object" "from_content" {
  project_id   = neon_bucket.example.project_id
  branch_id    = neon_bucket.example.branch_id
  bucket       = neon_bucket.example.name
  key          = "hello.txt"
  content      = "Hello, World!"
  content_type = "text/plain"
}

resource "neon_bucket_object" "from_content_base64" {
  project_id     = neon_bucket.example.project_id
  branch_id      = neon_bucket.example.branch_id
  bucket         = neon_bucket.example.name
  key            = "hello_base64.txt"
  content_base64 = base64encode("Hello, World!")
}

resource "neon_bucket_object" "from_file" {
  project_id = neon_bucket.example.project_id
  branch_id  = neon_bucket.example.branch_id
  bucket     = neon_bucket.example.name
  key        = "object"
  source     = "/path/to/file"
}

# use the data source to update the object's content in the bucket
# by tracking it using the source and its md5 hashsum as a trigger
data "local_file" "source" {
  filename = "/path/to/file/foo.bar"
}

resource "neon_bucket_object" "example" {
  project_id = neon_bucket.example.project_id
  branch_id  = neon_bucket.example.branch_id
  bucket     = neon_bucket.example.name
  key        = "object"
  source     = data.local_file.source.filename
  trigger    = data.local_file.source.content_md5
}

# provision a prefix, a/k/a folder by setting the attr. is_directory to true
resource "neon_bucket_object" "example" {
  project_id   = neon_bucket.example.project_id
  branch_id    = neon_bucket.example.branch_id
  bucket       = neon_bucket.example.name
  key          = "folder"
  is_directory = true
}
