# supported Neon regions
data "neon_active_regions" "this" {}

output "region_ids" {
  value = [for region in data.neon_active_regions.this.regions : region.region_id]
}

# supported Neon regions for a given Organization
data "neon_active_regions" "org" {
  org_id = "org-foo-bar-12345678"
}

output "region_ids_org" {
  value = [for region in data.neon_active_regions.org.regions : region.region_id]
}
