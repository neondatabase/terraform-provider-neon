# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Added the resources `neon_auth`, `neon_auth_trusted_domain` and `neon_auth_oauth_provider` to enable Neon Auth (Managed Better Auth) on a branch, trust redirect domains, and configure OAuth providers with your own credentials.

## [v0.19.0-beta.0] - 2026-11-06

### Added

- Added the following resources:
  - [[#27](https://github.com/neondatabase/terraform-provider-neon/issues/27)] `neon_project_member_role`;
  - [[#27](https://github.com/neondatabase/terraform-provider-neon/issues/27)] `neon_org_member_role`.
  - [[#29](https://github.com/neondatabase/terraform-provider-neon/issues/29)] `neon_function`;
  - [[#30](https://github.com/neondatabase/terraform-provider-neon/issues/30)] `neon_bucket`;
  - [[#30](https://github.com/neondatabase/terraform-provider-neon/issues/30)] `neon_bucket_object`;
  - [[#34](https://github.com/neondatabase/terraform-provider-neon/issues/34)] `neon_trigger`;
  - [[#35](https://github.com/neondatabase/terraform-provider-neon/issues/35)] `neon_service_credential`;
  - [[#36](https://github.com/neondatabase/terraform-provider-neon/issues/36)] `neon_custom_branch_domain`;
  - [[#39](https://github.com/neondatabase/terraform-provider-neon/issues/39)] `neon_org_spending_limit`;
  - [[#40](https://github.com/neondatabase/terraform-provider-neon/issues/40)] `neon_data_api`;
  - [[#41](https://github.com/neondatabase/terraform-provider-neon/issues/41)] `neon_snapshot`;

- Added the following data sources:
  - [[#43](https://github.com/neondatabase/terraform-provider-neon/issues/43)] `neon_active_regions`;
  - [[#45](https://github.com/neondatabase/terraform-provider-neon/issues/45)] `neon_ai_gateway`;
  - [[#46](https://github.com/neondatabase/terraform-provider-neon/issues/46)] `neon_bucket`.

- [[#51](https://github.com/neondatabase/terraform-provider-neon/issues/51)] Added the following ephemeral resources:
  - `neon_role`;
  - `neon_connection_uri`.

- Added the following attributes to the resource `neon_endpoint`:
  - `passwordless_access`;
  - `enabled_libraries`;
  - `enabled_libraries_use_defaults`.

- Added the following attributes to the resource `neon_project`:
  - `enabled_libraries`;
  - `enabled_libraries_use_defaults`.
  - `branch.project_id`;
  - `branch.parent_id`;
  - `branch.parent_lsn`;
  - `branch.parent_timestamp`;
  - `branch.logical_size`;
  - `branch.protected`;
  - `primary_compute.project_id`;
  - `primary_compute.branch_id`;
  - `primary_compute.region_id`;
  - `primary_compute.type`;
  - `primary_compute.compute_provisioner`;
  - `primary_compute.disabled`;
  - `primary_compute.enabled_libraries`;
  - `primary_compute.enabled_libraries_use_defaults`;
  - `primary_compute.passwordless_access`;
  - `primary_compute.host`;
  - `primary_compute.host_pooler`.

- Added the attribute `filter` to the data source `neon_branches`.

- Added import to the resource `neon_jwks_url`, with the ID `<project_id>/<jwks_id>`.

- Added the following attributes to the elements of the `branches` list in the data source `neon-branches`:
  - `project_id`;
  - `parent_lsn`;
  - `parent_timestamp`;
  - `protected`.

- Added the following attribute to the data source `neon_project`:
  - `allowed_ips`;
  - `allowed_ips_protected_branches_only`;
  - `autoscaling_limit_max_cu`;
  - `autoscaling_limit_min_cu`;
  - `block_public_connections`;
  - `block_vpc_connections`;
  - `compute_provisioner`;
  - `default_endpoint_id`;
  - `enable_logical_replication`;
  - `hipaa`;
  - `history_retention_seconds`;
  - `org_id`;
  - `pg_version`;
  - `region_id`;
  - `store_password`;
  - `suspend_timeout_seconds`;
  - `enabled_libraries`;
  - `enabled_libraries_use_defaults`;
  - `branch.id`;
  - `branch.name`;
  - `branch.project_id`;
  - `branch.parent_id`;
  - `branch.parent_lsn`;
  - `branch.parent_timestamp`;
  - `branch.logical_size`;
  - `branch.protected`;
  - `maintenance_window.end_time`;
  - `maintenance_window.start_time`;
  - `maintenance_window.weekdays`;
  - `primary_compute.autoscaling_limit_max_cu`;
  - `primary_compute.autoscaling_limit_min_cu`;
  - `primary_compute.id`;
  - `primary_compute.name`;
  - `primary_compute.suspend_timeout_seconds`;
  - `primary_compute.project_id`;
  - `primary_compute.branch_id`;
  - `primary_compute.region_id`;
  - `primary_compute.type`;
  - `primary_compute.compute_provisioner`;
  - `primary_compute.disabled`;
  - `primary_compute.enabled_libraries`;
  - `primary_compute.enabled_libraries_use_defaults`;
  - `primary_compute.passwordless_access`;
  - `primary_compute.host`;
  - `primary_compute.host_pooler`;
  - `quota.active_time_seconds`;
  - `quota.compute_time_seconds`;
  - `quota.data_transfer_bytes`;
  - `quota.logical_size_bytes`;
  - `quota.written_data_bytes`.

### Changed

- [[#28](https://github.com/neondatabase/terraform-provider-neon/issues/28)] Migrated the provider to Terraform Plugin Framework from Terraform Plugin SDK v2.
  State written by `kislerdm/neon` v0.18.0 is upgraded in place on the first plan; upgrade to v0.18.0 first when coming from an older version.
- Changed the following attributes of the resource `neon_project`:
  - **[BREAKING]** `branch` is the nested attribute instead of block now:
  ```hcl
  resource "neon_project" "_" {
    branch = {
      // default branch' attributes go here
    }
  }
  ```
  - **[BREAKING]** `primary_compute` is the nested attribute instead of block now:
  ```hcl
  resource "neon_project" "_" {
    primary_compute = {
    // primary compute's attributes go here
    }
  }
  ```
  - **[BREAKING]** `maintenance_window` is the nested attribute instead of block now:
  ```hcl
  resource "neon_project" "_" {
    maintenance_window = {
      // maintenance window attributes go here
    }
  }
  ```
  - **[BREAKING]** `quota` is the nested attribute instead of block now:
  ```hcl
  resource "neon_project" "_" {
    quota = {
      // quota attributes go here
    }
  }
  ```
  - **[BREAKING]** `allowed_ips_protected_branches_only`, `block_public_connections`, `block_vpc_connections`,
    `enable_logical_replication`, `hipaa` and `store_password` take `true` or `false` instead of `"yes"` or `"no"`.
- **[BREAKING]** `protected` of the resource `neon_branch` takes `true` or `false` instead of `"yes"` or `"no"`.
- **[BREAKING]** Renamed the attribute `host_pooling` of the resource `neon_endpoint` to `host_pooler`.
- Changing `type` of the resource `neon_endpoint` replaces the endpoint.
- Removing `enabled_libraries`, `enabled_libraries_use_defaults` or `suspend_timeout_seconds` from a `neon_project`, `neon_project.primary_compute` or `neon_endpoint` configuration keeps the current value, including when another attribute changes. Set `enabled_libraries = []` to clear the libraries.
- **[BREAKING]** `branch_id` and `jwt_audience` of the resource `neon_jwks_url` reject empty strings. Omit them instead.
- Neon API errors name the cause in the summary (rejected API key, missing resource, rejected request, rate limit,
  API outage) and end with the next step to take.
- See the [migration guide](docs/guides/migrate-from-kislerdm-neon.md) to move existing state from `kislerdm/neon`.

- Updated dependencies:
  - github.com/hashicorp/terraform-plugin-docs: [v0.25.0](https://github.com/hashicorp/terraform-plugin-docs/compare/v0.21.0...v0.25.0)
  - github.com/hashicorp/terraform-plugin-framework: [v1.19.0](https://github.com/hashicorp/terraform-plugin-framework/compare/v1.14.1...v1.19.0)
  - github.com/hashicorp/terraform-plugin-go: [v0.31.0](https://github.com/hashicorp/terraform-plugin-go/compare/v0.26.0...v0.31.0)
  - github.com/hashicorp/terraform-plugin-log: [v0.11.0](https://github.com/hashicorp/terraform-plugin-log/compare/v0.9.0...v0.11.0)
  - github.com/hashicorp/terraform-plugin-sdk/v2: [v2.40.1](https://github.com/hashicorp/terraform-plugin-sdk/v2/compare/v2.36.1...v2.40.1)
  - Neon Go SDK: [v0.36.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.22.0...v0.36.0)

### Deprecated

- Deprecated the following fields of the resource `neon_project`: 
  - `default_branch_id`: use `branch.id` instead;
  - `default_endpoint_id`: use `primary_compute.id` instead;
  - `database_host`: use `primary_compute.host` instead;
  - `database_host_pooler`: use `primary_compute.host_pooler` instead.

### Removed

- **[BREAKING]** Removed the attribute `default_branch_protected` from the resource `neon_project`. Use `branch.protected` instead.
- [[#51](https://github.com/neondatabase/terraform-provider-neon/issues/51)] **[BREAKING]** Removed the read-only attribute `password` from the resource `neon_role`.
- [[#51](https://github.com/neondatabase/terraform-provider-neon/issues/51)] **[BREAKING]** Removed the data source `neon_branch_role_password`.
- [[#51](https://github.com/neondatabase/terraform-provider-neon/issues/51)] **[BREAKING]** Removed the read-only attributes `connection_uri`, `connection_uri_pooler` and `database_password` from the resource and data-source `neon_project`.
- **[BREAKING]** Removed the attributes `database_name`, `role_name` from the nested attribute `branch` of the resource `neon_project`.
- **[BREAKING]** Removed the attributes `proxy_host` and `pg_settings` from the resource `neon_endpoint`. Note that `pg_settings` is removed temporarily until the Neon API is fixed.
- **[BREAKING]** Removed the attributes `primary` from the element of the `branches` list in the data-source `neon_branches`.

### Fixed

- `expires_at` and `timestamp` of `neon_snapshot` keep the configured RFC 3339 offset when Neon returns the same instant in UTC, instead of failing with an inconsistent result.
- `neon_bucket_object` accepts an `is_directory` value that is unknown at plan time.
- Omitting `retention_seconds` in a `neon_branch_backup_schedule` uses Neon's default of 3024000 seconds instead of planning a change on every run.
- Importing a `neon_bucket_object` whose key contains `%`, `?` or `#` works instead of crashing the provider, and the first plan after an import updates the object in place instead of replacing it. Import keeps the key as given and imports a folder when only its placeholder object exists.
- `environment_variables` of `neon_function` rejects empty values, which Neon treats as deleting the variable. After a deployment that Neon accepted but that did not become active, the next plan deploys the configured inputs again, even if the configuration was reverted, and deletes the variables that deployment added.
- `passwordless_access` of `neon_endpoint` is sent on create. Neon does not support creating or updating an endpoint with it set to `false` yet, and the error now says so instead of the apply failing with an inconsistent result.
- A `neon_database` rename is recorded in state even if waiting for the operation afterwards fails, so the next refresh does not lose the database.
- `neon_org_member_role` is removed from state when the member left the organization outside Terraform, instead of failing the refresh.
- A `neon_function` update whose deployment became active records the deployed inputs even if the rename or the read after it fails, so a later change still reaches Neon.
- `neon_jwks_url` is removed from state when its project was deleted outside Terraform, instead of failing the refresh.
- Updating `label` of a `neon_vpc_endpoint_assignment` created with v0.18.0 keeps its ID instead of failing with an inconsistent result.
- `compute_provisioner` of `neon_project` and `neon_endpoint` has no default. A configuration that omits it keeps the provisioner in state, so projects created with `k8s-pod` are not replaced.
- `neon_trigger` accepts `cron` and `bucket_name` values that are unknown at plan time, and checks them against `type` on create as well as on update.
- `environment_variables` of `neon_function` accepts values that are unknown at plan time.
- A resource whose create call succeeded but whose follow-up operation failed is kept in state and marked tainted, so Terraform can replace or destroy it. This covers `neon_project`, `neon_branch`, `neon_endpoint`, `neon_role`, `neon_database`, `neon_jwks_url` and `neon_function`.
- Changing `parent_id`, `parent_lsn` or `parent_timestamp` of `neon_branch` to a value that is unknown at plan time, for example the ID of a replaced parent branch, plans a replacement instead of failing during apply. Setting `parent_lsn` and `parent_timestamp` together is rejected at plan time, including on create.
- An organization API key used for an operation that requires a personal API key no longer removes the resource from state as if it were deleted. The error says to use a personal API key.
- Neon operations that end with the status `failed`, `error` or `cancelled` now fail the apply instead of being treated as finished, and waiting for operations stops when Terraform is interrupted.
- [[#52](https://github.com/neondatabase/terraform-provider-neon/issues/52)] Fixed silent removal of the `neon_project` resource from the state if the API returns the status code 422, e.g., when the project contains protected branches.
- [[#59](https://github.com/neondatabase/terraform-provider-neon/issues/59)] Fixed the provider's attribute `api_key` security flag.
- [[#60](https://github.com/neondatabase/terraform-provider-neon/issues/60)] Fixed state management of the project's `maintenance_window`.

## [v0.18.0] - 2026-09-12

### Fixed

- Fixed the update logic for the `autoscaling_limit_min_cu` attribute of the `neon_project` resource so that it now reaches the API. The min-CU update was gated on a change to `autoscaling_limit_max_cu`.
- Fixed the deletion logic of the `neon_branch_backup_schedule` resource.
- Fixed the password reading of the `neon_role` resource.
- Fixed the log message when reading non-existing roles.

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.22.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.20.0...v0.22.0)

## [v0.17.0] - 2026-09-08

### Added

- [[#202](https://github.com/kislerdm/terraform-provider-neon/issues/202)] Added the resource `neon_branch_backup_schedule` to configure backup schedules for project branches.

## [v0.16.0] - 2026-09-07

### Added

- Added attributes `autoscaling_limit_min_cu`, `autoscaling_limit_max_cu`, `suspend_timeout_seconds` to the `neon_project` resource to configure defaults for the project's compute resources.
- [[#234](https://github.com/kislerdm/terraform-provider-neon/issues/234)] Added the attr. block `primary_compute` to the resource `neon_project` to configure the primary compute resources provisioned with the project.
- Added read-only attribute `host_pooling` to the resource `neon_endpoint`.
- Added the attribute `name` to the resource `neon_endpoint`.

### Removed

- **[BREAKING]** Removed the attr. block `default_endpoint_settings` from the resource `neon_project`.
Instead, use the `neon_project` attributes `autoscaling_limit_min_cu`, `autoscaling_limit_max_cu`, `suspend_timeout_seconds` to configure defaults for the project's compute resources.
- **[BREAKING]** Removed the attribute `pooler_mode` from the resource `neon_endpoint` because it's no longer supported by the Neon API.
- **[BREAKING]** Removed the attribute `pooler_enabled` from the resource `neon_endpoint` because the pooling is [always activated](https://neon.com/docs/connect/connection-pooling#enable-disable-and-find-the-pooled-connection-string-in-the-console:~:text=The%20pooled%20endpoint%20is%20always%20available).

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.20.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.16.0...v0.20.0)

## [v0.15.0] - 2026-08-02

### Fixed

- Fixed the link in the JWKS URL resource's documentation.

### Changed

- [[209](https://github.com/kislerdm/terraform-provider-neon/issues/209)] Changed the provider's state management behaviour. Now, the provider will not update the state of the resources if the resource's attributes are not changed in the Terraform configuration. It will attempt to recreate sources deleted outside terraform only if the resource's attributes are changed in the Terraform configuration. This change is introduced to avoid unexpected recreation of resources when they are deleted outside Terraform.

## [v0.14.0] - 2026-07-14

### Added

- [[203](https://github.com/kislerdm/terraform-provider-neon/issues/203)] Added the attribute `hipaa` to the resource `neon_project` to configure the project's HIPAA compliance.
- [[221](https://github.com/kislerdm/terraform-provider-neon/issues/221)] Added the attribute `default_branch_protected` to the resource `neon_project` to configure protection of the default branch.

### Fixed

- [[218](https://github.com/kislerdm/terraform-provider-neon/issues/218)] Fixed state management for the attribute `enable_logical_replication` of the resource `neon_project`. It can be changed in-place now.
- [[223](https://github.com/kislerdm/terraform-provider-neon/issues/223)] Fixed state management for the attribute `maintenance_window` of the resource `neon_project`. It will be updated only if it's changed in the tf state change.
- Fixed state management for the attribute `default_endpoint_settings.suspend_timeout_seconds` of the resource `neon_project`. Now, it can be set to `-1` to turn auto-suspension off.

## [v0.13.0] - 2026-01-02

### Added

- Added the following attributes to the resource `neon_project`:
  - `block_public_connections`;
  - `block_vpc_connections`.

### Fixed

- [[191](https://github.com/kislerdm/terraform-provider-neon/issues/191)] Fixed the import logic for the resource
  `neon_vpc_endpoint_assignment`.
- Fixed documentation for the resource `neon_project_permission`.
- [[193](https://github.com/kislerdm/terraform-provider-neon/issues/193)] Fixed the import logic for the resource
  `neon_endpoint`.
- [[198](https://github.com/kislerdm/terraform-provider-neon/issues/198)] Fixed the import logic for the resource
  `neon_branch`.

### Changed

- **[BREAKING]** Changed the `id` attribute of the `neon_project_permission`. It's identical to the identifier assigned
  by Neon now.

## [v0.12.0] - 2025-11-04

### Added

- [[#126](https://github.com/kislerdm/terraform-provider-neon/issues/126)] Added the block `maintenance_window` to the `neon_project` resource to configure the [Neon maintenance window](https://neon.com/docs/manage/updates#updates-on-paid-plans).
- Added examples to the documentation of the resource `neon_org_api_key`.

## [v0.11.0] - 2025-10-26

### Added

- [[#148](https://github.com/kislerdm/terraform-provider-neon/issues/148)] Added the resource `neon_org_api_key` to manage API keys on per org. level.

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.16.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.13.0...v0.16.0)

## [v0.10.0] - 2025-10-12

### Removed

- [[#157](https://github.com/kislerdm/terraform-provider-neon/issues/157)] Removed the client-side validation of the autoscaling limit input.
- Removed client-side validation of the Postgres version input.

### Fixed

- [[#154](https://github.com/kislerdm/terraform-provider-neon/issues/154)], [[#156](https://github.com/kislerdm/terraform-provider-neon/issues/156)] Fixed projects listing when importing resources.
- [[#166](https://github.com/kislerdm/terraform-provider-neon/issues/166)] Fixed the `neon_project` resource validation to configure the default endpoint to never suspend.
- [[#179](https://github.com/kislerdm/terraform-provider-neon/issues/179)] Fixed the `neon_project` resource diff management when the `org_id` is attribute is not set in the terraform module.

## [v0.9.0] - 2025-02-25

### Added

- Added the resources `neon_vpc_endpoint_assignment` and `neon_vpc_endpoint_restriction` to manage Neon Private Networking.

## [v0.8.0] - 2025-02-24

### Added

- [[#144](https://github.com/kislerdm/terraform-provider-neon/issues/144)] Added the resource `neon_jwks_url`
  to manage the JWKs URL provided by the 3rd-party IdP required to establish [Neon RLS authorization](https://neon.tech/docs/guides/neon-rls-authorize).

### Changed

- Updated the Go version: 1.23.6 from 1.22.7.

## [v0.7.1] - 2025-02-19

### Fixed

- Fixed the note about the endpoints types.

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.13.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.12.0...v0.13.0)

## [v0.7.0] - 2025-02-19

### Added

- Added the "User-Agent" header injected to every request to the Neon API for tracking purposes as agreed with
  James Broadhead from Neon.
- Added the resource `neon_api_key` to manage Neon API keys.
- Added the output attributes `connection_uri_pooler` and `database_host_pooler` to the resource `neon_project` to connect to the default database in the pooler mode.

### Fixed

- [[#119](https://github.com/kislerdm/terraform-provider-neon/issues/119)] Fixed the output attribute `host` of the
  resource `neon_endpoint`: it will yield the correct URI for the endpoints with the
  [pooled mode](https://neon.tech/docs/connect/connection-pooling#how-to-use-connection-pooling) activated.
- [[#137](https://github.com/kislerdm/terraform-provider-neon/issues/137)] Fixed operations execution management
  by introducing await mechanism to wait until the running operations finish.
- [[#133](https://github.com/kislerdm/terraform-provider-neon/issues/133)] Fixed state management when configuring network security.
- Documentation improvements:
  - Removed unclear warning from the page for the `neon_endpoint` resource.

### Removed

- Removed the outdated warning which was showing upon creation of the endpoint of the type "read_only",
- Removed the attributes "allowed_ips_primary_branch_only" from the resource `neon_project` because it's no longer supported by Neon.

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.12.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.6.1...v0.12.0)
  - github.com/hashicorp/terraform-plugin-docs: [v0.20.1](https://github.com/hashicorp/terraform-plugin-docs/compare/v0.19.4...v0.20.1)
  - github.com/hashicorp/terraform-plugin-sdk: [v2.36.0](https://github.com/hashicorp/terraform-plugin-sdk/compare/v2.33.0...v2.36.0)
- **[BREAKING]** [#113](https://github.com/kislerdm/terraform-provider-neon/issues/113)] Set the default retention window to 1 day to avoid inconsistency with Neon.

## [v0.6.3] - 2024-10-05

### Fixed

- Fixed docu; non-functional change.

## [v0.6.2] - 2024-10-04

### Added

- Added the attribute `protected` for the resource `neon_branch` to provision protected branches.

### Fixed

- [[#108](https://github.com/kislerdm/terraform-provider-neon/issues/108)] Fixed the import behaviour for the resource `neon_role`.
- Fixed mutability of the default branch by adjusting the behaviour for the `branch` state of the resource `neon_project`.

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.6.1](https://github.com/kislerdm/neon-sdk-go/compare/v0.5.0...v0.6.1)

## [v0.6.1] - 2024-09-28

### Added

- Added support of Postgres 17. See the Neon [announcement](https://neon.tech/blog/postgres-17) and the Postgres
  [announcement](https://www.postgresql.org/about/news/postgresql-17-released-2936/).

### Fixed

- Fixed validation of the autoscalling limits. You can now set the maximum compute size up to `10`.

## [v0.6.0] - 2024-09-23

### Added

- [[#99](https://github.com/kislerdm/terraform-provider-neon/issues/99)] Added the attribute `org_id` to
  the resource `neon_project` to create project in the organisation.

### Fixed

- **[BREAKING]** [[#96](https://github.com/kislerdm/terraform-provider-neon/issues/96)] The boolean attributes of the
  resource `neon_project` will be treated as strings to work around the
  [issue](https://github.com/hashicorp/terraform-plugin-sdk/issues/817) with state management when the attribute gets
  removed from the manifest.

**Examples**

- Set allowed_ips to be applicable only to the primary branch:
  ```terraform
   resource "neon_project" "this" {
      name = "myproject"

      allowed_ips = ["1.2.3.4/24"]

      allowed_ips_primary_branch_only = "yes"
   }
  ```
- Set allowed_ips to be applicable to all branches, explicitly:
  ```terraform
   resource "neon_project" "this" {
      name = "myproject"

      allowed_ips = ["1.2.3.4/24"]

      allowed_ips_primary_branch_only = "no"
   }
  ```
- Set allowed_ips to be applicable to all branches, implicitly:
  ```terraform
   resource "neon_project" "this" {
      name = "myproject"

      allowed_ips = ["1.2.3.4/24"]
   }
  ```

### Changed

- Updated dependencies:
  - Neon Go SDK: [v0.5.0](https://github.com/kislerdm/neon-sdk-go/compare/v0.4.7...v0.5.0)
  - github.com/hashicorp/terraform-plugin-docs: [v0.19.4](https://github.com/hashicorp/terraform-plugin-docs/compare/v0.19.0...v0.19.4)

## [v0.5.0] - 2024-03-10

### Added

- [[#22](https://github.com/kislerdm/terraform-provider-neon/issues/22)] Added the following data resources:
    - `neon_project`
    - `neon_branches`
    - `neon_branch_endpoints`
    - `neon_branch_roles`
    - `neon_branch_role_password`

- Added the read-only attribute `default_endpoint_id` to the resource `neon_project`.
- Added the retry logic to manage all supported resources:
    - `neon_project`
    - `neon_branch`
    - `neon_endpoint`
    - `neon_role`
    - `neon_database`
    - `neon_project_permission`

### Fixed

- [[#83](https://github.com/kislerdm/terraform-provider-neon/issues/83)] Fixed the state management of the project's
  default branch, role, database and endpoint.
- [[#88](https://github.com/kislerdm/terraform-provider-neon/issues/88)] Fixed import of the resource `neon_role`.

### Changed

- Updated dependencies:
    - Neon Go SDK [v0.4.7](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.7)
- Reduced the retry delay to _1 second_ from _5 seconds_.

## [v0.4.1] - 2024-02-28

### Changed

- Updated dependencies:
    - Neon Go SDK [v0.4.6](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.6)
    - terraform-plugin-sdk [v2.33.0](https://github.com/hashicorp/terraform-plugin-sdk/releases/tag/v2.33.0)
    - Google UUID module [v1.6.0](https://github.com/google/uuid/releases/tag/v1.6.0)

## [v0.4.0] - 2024-01-28

### Added

- Added the resource `neon_project_permission` to manage the project's permissions.

### Changed

- Updated dependencies:
    - Neon Go SDK [v0.4.3](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.3)
    - Terraform docs [v0.18.0](https://github.com/hashicorp/terraform-plugin-docs/releases/tag/v0.18.0)

## [v0.3.2] - 2024-01-11

### Fixed

- [`resource_project>region_id`] Validation of the project deployment region was removed.

### Changed

- Updated dependencies: Neon Go SDK [v0.4.2](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.2)

## [v0.3.1] - 2023-12-22

### Added

- `resource_project` includes the attribute `enable_logical_replication` to configure the [logical replication](https://neon.tech/docs/introduction/logical-replication).

### Fixed

- PostgreSQL 16 is now supported.

### Changed

- Updated dependencies: Neon Go SDK [v0.4.1](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.1)

## [v0.3.0] - 2023-12-21

### Added

- `resource_project` includes two additional attributes to configure IP addresses allowed to connect to the project's
  endpoints:
    - `allowed_ips`
    - `allowed_ips_primary_branch_only`

### Fixed

- Schema is set on per resource basis now.

### Changed

- Updated dependencies:
    - Go version to 1.21
    - Neon Go SDK [v0.4.0](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.4.0)
    - Terraform plugin SDK [v2.31.0](https://github.com/hashicorp/terraform-plugin-sdk/releases/tag/v2.31.0)

## [v0.2.5] - 2023-11-02

### Fixed

- Fixed validation of the [branch ID](https://neon.tech/docs/manage/branches#view-branches).

## [v0.2.4] - 2023-10-26

### Fixed

- Minor documentation fixes:
    - The note in the `resource_role` is removed because it's not reflecting the provider's behaviour.
    - The logo is fixed.

### Changed

- Updated dependencies: Neon Go SDK [v0.3.0](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.3.0)

## [v0.2.3] - 2023-10-22

### Fixed

- [[#51](https://github.com/kislerdm/terraform-provider-neon/issues/51)] Fixed credentials content.
- Fixed management of the `resource_role` state:
    - Fixed password reading.
    - Removed the side effect upon the resource import: the role's password won't be reset now.

### Changed

- Updated dependencies: Neon Go SDK [v0.2.5](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.2.5)

## [v0.2.2] - 2023-09-19

### Fixed

- `resource_project`:
    - ([#40](https://github.com/kislerdm/terraform-provider-neon/issues/40)) Fixed types conversion for the terraform
      resource attributes of the
      type [`TypeInt`](https://github.com/hashicorp/terraform-plugin-sdk/blob/af738e0d482f699504d9e35e134766da459ef1f6/helper/schema/schema.go#L55).
    - ([#42](https://github.com/kislerdm/terraform-provider-neon/issues/42)) Fixed default branch configuration.
    - ([#48](https://github.com/kislerdm/terraform-provider-neon/issues/48)) Fixed default endpoint settings
      configuration.
    - Fixed history retention configuration. Now, the retention period of 7 days will be set by default, and zero will
      be set if `history_retention_seconds` is set to zero explicitly.

**Note** web console will reflect the data retention period correctly only if `history_retention_seconds` was set to an
integral number of days because the web console shows the total number of full days only. Moreover, "7 days - default"
will be displayed for any `history_retention_seconds` value below 86400 (1 day).

| history_retention_seconds | web console      | human-friendly duration |
|:--------------------------|:-----------------|:------------------------|
| 0                         | 7 days - default | 0                       |
| 300                       | 7 days - default | 5 min                   |
| 3600                      | 7 days - default | 1 hour                  |
| 43200                     | 7 days - default | 12 hours                |
| 164160                    | 1 day            | 1.9 days                |
| 198720                    | 2 days           | 2.3 days                |
| 604800                    | 7 days - default | 7 days                  |

- Documentation:
    - Link to the Neon logo
    - End-to-end example

### Changed

- Documentation: examples of project provisioning
- Updated dependencies: Neon Go SDK [v0.2.2](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.2.2),
  terraform plugin SDK to 2.29.0

## [0.2.1] - 2023-08-07

### Added

- Acceptance e2e tests
- `resource_project`:
    - default_branch_id

- `resource_endpoint`:
    - id
    - compute_provisioner
    - suspend_timeout_seconds

### Removed

- `resource_role`:
    - created_at
    - updated_at

- `resource_endpoint`:
    - passwordless_access: it's not implemented yet by Neon
    - current_state
    - pending_state

- `resource_branch`:
    - connection_uri

### Changed

- `resource_endpoint`:
    - autoscaling_limit_min_cu set to 0.25 by default
    - autoscaling_limit_max_cu set to 0.25 by default
    - type set to "read_write" by default

## [0.2.0] - 2023-08-04

The release follows update of the [Neon Go SDK](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.2.1).

### Fixed

- ([#25](https://github.com/kislerdm/terraform-provider-neon/issues/25)) Fixed branch import
- ([#26](https://github.com/kislerdm/terraform-provider-neon/issues/26)) Fixed database import
- ([#32](https://github.com/kislerdm/terraform-provider-neon/issues/32)) Data type to define autoscaling limits
- Neon logo in documentation

### Added

- `resource_project`:
    - store_password (_Note that Neon does not support "false" value yet_)
    - history_retention_seconds
    - compute_provisioner
    - quota:
        - active_time_seconds
        - compute_time_seconds
        - written_data_bytes
        - data_transfer_bytes
        - logical_size_bytes
    - default_endpoint_settings:
        - autoscaling_limit_min_cu
        - autoscaling_limit_max_cu
        - suspend_timeout_seconds
    - branch:
        - id
        - name
        - role_name
        - database_name

- `resource_branch`:
    - id
    - connection_uri

### Removed

- `resource_project`:
    - pg_settings
    - cpu_quota_sec
    - autoscaling_limit_min_cu
    - autoscaling_limit_max_cu
    - branch_logical_size_limit
    - created_at
    - updated_at

- `resource_branch`:
    - physical_size_size
    - endpoint
    - host
    - current_state
    - pending_state
    - created_at
    - updated_at

- `resource_database`:
    - created_at
    - updated_at

- `resource_endpoint`:
    - created_at
    - updated_at

## [0.1.0] - 2023-01-08

### Changed

- Fixed typo and indentation of documentation

## [0.0.9] - 2023-01-08

### Fixed

- Fixed `neon_branch` recourse by provisioning an endpoint attached to a newly created branch. It is required to permit
  interactions with the branch to manage associated roles and databases.

### Changed

- Improved documentation
- Added an end-to-end guide to provision resources for AWS application to communicate with the Neon database

## [0.0.8] - 2023-01-08

### Added

- Database resource:

```terraform
resource "neon_database" "this" {
  project_id = "bitter-meadow-966132"
  branch_id  = "br-floral-mountain-251143"
  name       = "baz"
  owner_name = "qux"
}
```

## [0.0.7] - 2023-01-08

### Added

- Role resource:

```terraform
resource "neon_role" "this" {
  project_id = "bitter-meadow-966132"
  branch_id  = "br-floral-mountain-251143"
  name       = "qux"
}
```

## [0.0.6] - 2023-01-08

### Fixed

- Fixed `neon_endpoint` resource provisioning when the attribute `pg_settings` is not set. The bug was in the Neon SDK,
  see
  details in the [release notes](https://github.com/kislerdm/neon-sdk-go/releases/tag/v0.1.4).

### Changed

- Bumped [Neon Go SDK](https://pkg.go.dev/github.com/kislerdm/neon-sdk-go) to v0.1.4

## [0.0.5] - 2023-01-07

### Fixed

- Fixed the logic to import `neon_branch` resource by its ID.

## [0.0.4] - 2023-01-07

### Added

- Endpoint resource:

```terraform
resource "neon_endpoint" "this" {
  project_id = "bitter-meadow-966132"
  branch_id  = "br-floral-mountain-251143"
  type       = "read_write"
}
```

## [0.0.3] - 2023-01-07

### Added

- Added database connection details for the resource `neon_project`. **Note** that `database_password`
  and `connection_uri` read-only attributes are sensitive. Make sure that terraform backend is secured and
  terraform state is not exposed to prevent undesirable access to database.

## [0.0.2] - 2023-01-07

### Added

- Branch resource:

```terraform
resource "neon_project" "this" {
  name = "foo"
}

resource "neon_branch" "this" {
  project_id = neon_project.this.id
  name       = "bar"
}
```

- Backoff+retry mechanism: operation is retried after the delay of 5 sec. API response's HTTP codes are 500, or 429.
  Total number of attempts is limited to 120 per operation.

### Changed

- Bumped [Neon Go SDK](https://pkg.go.dev/github.com/kislerdm/neon-sdk-go) v0.1.3
- Added errors handling for the project resource

## [0.0.1] - 2023-01-05

### Added

- `Neon` Provider:

```terraform
terraform {
  required_providers {
    neon = {
      source = "kislerdm/neon"
    }
  }
}

provider "neon" {}
```

- Project resource:

```terraform
resource "neon_project" "this" {
  name = "foo"
}
```
