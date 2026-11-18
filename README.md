# Terraform Provider Neon

The official Terraform provider for [Neon](https://neon.com/). Manage Lakebase Postgres projects, branches, computes,
roles, and databases, plus Neon Functions, triggers, Object Storage, the Data API, Neon Auth, snapshots, API keys, and
access settings from HCL.

- [Registry documentation](https://registry.terraform.io/providers/neondatabase/neon/latest/docs)
- [Manage Neon with Terraform](https://neon.com/docs/reference/terraform) on neon.com
- [Changelog](CHANGELOG.md)

## Using the provider

```terraform
terraform {
  required_providers {
    neon = {
      source  = "neondatabase/neon"
      version = "0.19.0-beta.3"
    }
  }
}

provider "neon" {}
```

The provider is in beta. Terraform installs a beta release only when the version is pinned exactly.

The provider reads its API key from the `NEON_API_KEY` environment variable, or from `api_key` in the `provider` block.
Use an [organization API key](https://neon.com/docs/manage/api-keys) for most resources. `neon_api_key`,
`neon_org_api_key`, `neon_project_permission`, `neon_project_member_role`, `neon_org_member_role`, and the
`neon_active_regions` data source need a personal API key.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) 1.5 or later. Ephemeral resources need Terraform 1.10
  or later.

## Migrating from kislerdm/neon

`neondatabase/neon` continues the community provider `kislerdm/neon`. State written by `kislerdm/neon` v0.18.0 is
upgraded in place on the first plan; the configuration needs changes. Follow the
[migration guide](docs/guides/migrate-from-kislerdm-neon.md).

## Upgrading the provider

Run `terraform init -upgrade` by hand and review the plan before you apply it. In CI and auto-approved pipelines, run
`terraform init` without `-upgrade`, so that a new provider version can't replace resources unreviewed.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) covers building the provider, running the tests, generating the docs, and the
conventions for changes. AI coding agents start from [AGENTS.md](AGENTS.md).

## License

[Apache License 2.0](LICENSE).
