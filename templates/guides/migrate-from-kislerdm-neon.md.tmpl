---
page_title: "Migrate from kislerdm/neon"
---

# Migrate from kislerdm/neon

`neondatabase/neon` is the official Neon provider and continues the community provider `kislerdm/neon`. Your existing
projects, branches, endpoints, roles, and databases stay in place: the provider upgrades the Terraform state written by
`kislerdm/neon` v0.18.0 on the first plan, so nothing is imported again or recreated.

The configuration changes, though. This release moves the provider to the Terraform Plugin Framework, turns blocks into
nested attributes, replaces `"yes"`/`"no"` strings with booleans, and removes attributes that stored secrets in the state.

## Requirements

- Terraform 1.5 or later. Ephemeral resources, which replace the removed password attributes, need Terraform 1.10 or later.
- State written by `kislerdm/neon` v0.18.0. If you use an older version, upgrade to v0.18.0 and run `terraform apply`
  first, so that the state has the v0.18.0 shape.

## Steps

1. Change the provider source. Beta releases are only used when the version is pinned exactly:

   ```terraform
   terraform {
     required_providers {
       neon = {
         source  = "neondatabase/neon"
         version = "0.19.0-beta.1"
       }
     }
   }
   ```

2. Point the existing state at the new provider:

   ```shell
   terraform state replace-provider registry.terraform.io/kislerdm/neon registry.terraform.io/neondatabase/neon
   terraform init -upgrade
   ```

3. Update the configuration as described in [Configuration changes](#configuration-changes).

4. Review the plan:

   ```shell
   terraform plan
   ```

   After the configuration changes, the plan shows no changes, or only in-place updates. If it shows a resource being
   replaced or destroyed, don't apply it: compare the configuration with the tables below, or
   [open an issue](https://github.com/neondatabase/terraform-provider-neon/issues) with the plan output.

## Configuration changes

### `neon_project`

`branch`, `primary_compute`, `maintenance_window`, and `quota` are nested attributes now. Add `=` after the name:

```terraform
# kislerdm/neon v0.18.0
resource "neon_project" "this" {
  name = "app"

  branch {
    name          = "main"
    database_name = "app"
    role_name     = "app_owner"
  }
  default_branch_protected   = true
  enable_logical_replication = "yes"
}

# neondatabase/neon
resource "neon_project" "this" {
  name          = "app"
  database_name = "app"
  database_user = "app_owner"

  branch = {
    name      = "main"
    protected = true
  }
  enable_logical_replication = true
}
```

| v0.18.0 | Now |
|---|---|
| `branch { ... }`, `primary_compute { ... }`, `maintenance_window { ... }`, `quota { ... }` | `branch = { ... }` and so on |
| `branch.database_name`, `branch.role_name` | `database_name`, `database_user` |
| `default_branch_protected` | `branch.protected` |
| `allowed_ips_protected_branches_only`, `block_public_connections`, `block_vpc_connections`, `enable_logical_replication`, `hipaa`, `store_password` set to `"yes"` or `"no"` | `true` or `false` |
| `connection_uri`, `connection_uri_pooler`, `database_password` | the `neon_connection_uri` ephemeral resource |

References to `neon_project.this.branch[0].id` become `neon_project.this.branch.id`, and the same applies to the other
nested attributes. `default_branch_id`, `default_endpoint_id`, `database_host`, and `database_host_pooler` still work
and are deprecated: use `branch.id`, `primary_compute.id`, `primary_compute.host`, and `primary_compute.host_pooler`.

If you remove `default_branch_protected = true` and don't set `branch.protected`, the branch stays protected: the
provider keeps the value it reads from Neon.

### `neon_branch`

`protected` is a boolean: `"yes"` becomes `true`, and `"no"` becomes `false`.

### `neon_endpoint`

| v0.18.0 | Now |
|---|---|
| `host_pooling` | `host_pooler` |
| `proxy_host` | removed |
| `pg_settings` | removed until the Neon API supports it |

### `neon_role`

The `password` attribute is removed, so the password no longer ends up in the state. Read it with the `neon_role`
ephemeral resource, or use the `neon_connection_uri` ephemeral resource for a full connection string:

```terraform
ephemeral "neon_connection_uri" "app" {
  project_id = neon_project.this.id
  branch_id  = neon_project.this.branch.id
  database   = neon_project.this.database_name
  role       = neon_project.this.database_user
}
```

Pass `ephemeral.neon_connection_uri.app.uri` to a write-only argument of another resource, for example `value_wo` of
`aws_ssm_parameter`. See the [ephemeral resources documentation](https://developer.hashicorp.com/terraform/language/resources/ephemeral).

### `neon_jwks_url`

`branch_id` and `jwt_audience` no longer accept an empty string. If your configuration sets `branch_id = ""` or
`jwt_audience = ""`, remove the line; the plan then shows no changes. Leaving the empty string fails validation.

### Data sources

| v0.18.0 | Now |
|---|---|
| `data.neon_branch_role_password` | the `neon_role` ephemeral resource |
| `data.neon_project` attributes `connection_uri`, `connection_uri_pooler`, `database_password` | the `neon_connection_uri` ephemeral resource |
| `data.neon_branches` attribute `branches[*].primary` | `branches[*].default` |
