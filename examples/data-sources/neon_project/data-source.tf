data "neon_project" "example" {
  id = "cool-moon-42"
}

output "default_branch_id" {
  value = data.neon_project.example.branch.id
}

output "primary_compute_host" {
  value = data.neon_project.example.primary_compute.host
}
