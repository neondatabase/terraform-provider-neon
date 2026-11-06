data "neon_branch_roles" "this" {
  project_id = var.project_id
  branch_id  = var.branch_id
}

variable "project_id" {
  type = string
}

variable "branch_id" {
  type = string
}

output "roles" {
  value = data.neon_branch_roles.this.roles
}
