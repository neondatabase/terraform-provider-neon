resource "neon_custom_branch_domain" "api" {
  project_id  = neon_function.api.project_id
  branch_id   = neon_function.api.branch_id
  domain      = "api.example.com"
  entity_type = "function"
  entity_id   = neon_function.api.slug
}

output "api_cname_target" {
  value = neon_custom_branch_domain.api.cname_target
}
