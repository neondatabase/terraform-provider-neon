data "neon_branches" "example" {
  project_id = "cool-moon-42"
  filter     = "main"
}

output "branch_names" {
  value = [for branch in data.neon_branches.example.branches : branch.name]
}
