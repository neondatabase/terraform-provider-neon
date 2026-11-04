resource "neon_project" "example" {
  name      = "foo"
  region_id = "aws-us-east-2"
}

# credential to grant read-write permission for the Neon Object Storage
resource "neon_service_credential" "storageRW" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  scopes     = ["storage:read", "storage:write"]
}

# credential to grant read-only permission for the Neon Object Storage
resource "neon_service_credential" "storageRO" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  scopes     = ["storage:read"]
}

# credential to grant invocation permission for the Neon AI Gateway
resource "neon_service_credential" "storageAIGW" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  scopes     = ["ai_gateway:invoke"]
}

# credential to grant invocation permission for the Neon Functions
resource "neon_service_credential" "functions" {
  project_id = neon_project.example.id
  branch_id  = neon_project.example.branch.id
  scopes     = ["functions:invoke"]
}
