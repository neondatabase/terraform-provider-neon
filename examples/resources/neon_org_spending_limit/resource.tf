variable "neon_org_id" {
  type = string
}

resource "neon_org_spending_limit" "this" {
  org_id               = var.neon_org_id
  spending_limit_cents = 10000
}
