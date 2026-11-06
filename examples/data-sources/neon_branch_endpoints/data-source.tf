data "neon_branch_endpoints" "example" {
  project_id = "cool-moon-42"
  branch_id  = "br-cool-moon-42"
}

output "endpoint_hosts" {
  value = [for endpoint in data.neon_branch_endpoints.example.endpoints : endpoint.host]
}
