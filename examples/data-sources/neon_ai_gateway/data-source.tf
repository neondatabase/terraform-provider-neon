data "neon_ai_gateway" "this" {
  project_id = var.project_id
  branch_id  = var.branch_id
}

variable "project_id" {
  type = string
}

variable "branch_id" {
  type = string
}

output "base_url" {
  value = data.neon_ai_gateway.this.base_url
}
