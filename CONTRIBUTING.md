# Contributing to the Neon Terraform provider

This guide covers building the provider, running it locally, testing it against Neon, generating the docs, and the
conventions changes follow. If you use an AI coding agent, point it at [AGENTS.md](AGENTS.md) as well.

## Prerequisites

- [Go](https://go.dev/doc/install), at the version in [go.mod](go.mod).
- [Terraform](https://developer.hashicorp.com/terraform/install) 1.10 or later. The unit tests run Terraform, and the
  docs generator needs 1.10 to see ephemeral resources.
- [golangci-lint](https://golangci-lint.run/welcome/install/) for `make lint`.
- A Neon account and API keys for acceptance tests. See [Acceptance tests](#acceptance-tests).

## Repository layout

| Path | Contents |
| --- | --- |
| `provider/` | Resources (`resource_*.go`), data sources (`data_source_*.go`), ephemeral resources (`ephemeral_resource_*.go`), shared helpers, and all tests |
| `provider/helper.go` | Error diagnostics (`newDiagnostics`), schema helpers, and change detection |
| `provider/retry.go` | `projectReadiness`, the retry wrapper every Neon API call goes through |
| `provider/state_upgrade.go` | State upgraders for state written by `kislerdm/neon` v0.18.0 |
| `examples/` | HCL examples, embedded into the docs |
| `templates/` | Doc page templates for `tfplugindocs` |
| `docs/` | Generated registry docs. Don't edit by hand |
| `cmd/cleanup/` | Deletes leftover test projects by explicit ID |

## Build and run a local build

```sh
make build   # builds terraform-provider-neon_vdev
make test    # unit tests
make lint
```

To try a change in a real configuration, point Terraform at your build with a `dev_overrides` CLI config instead of
installing it:

```sh
go build -o "$PWD/bin/terraform-provider-neon" .
cat > dev.tfrc <<EOF
provider_installation {
  dev_overrides { "neondatabase/neon" = "$PWD/bin" }
  direct {}
}
EOF
export TF_CLI_CONFIG_FILE="$PWD/dev.tfrc"
```

With the override in place, skip `terraform init` for the provider and run `terraform plan` and `terraform apply`
directly. Keep `bin/`, `dev.tfrc`, state, and plans out of commits; state can hold credentials.

## How changes are made

### Keep current users working

Users run this provider against production Neon projects. A change that makes an existing configuration plan a
replacement, fail, or drift is a regression, even when the new behavior is better.

- **Schema shape.** Changing an attribute's type or nesting needs a new schema `Version` and an `UpgradeState` entry.
  Resources inherited from `kislerdm/neon` already carry a version above the one v0.18.0 wrote (`neon_project` 12,
  `neon_branch` and `neon_endpoint` 9, and so on); without an upgrader, the first plan fails with
  `Resource instance managed by newer provider version`.
- **Replacement.** Adding `RequiresReplace` to an existing attribute, or changing a default, can make existing
  resources plan a replacement. Check what `terraform plan` shows for state written by the previous release.
- **Migration from `kislerdm/neon`.** `TestMigrationFromKislerdm` applies a configuration with `kislerdm/neon` v0.18.0,
  moves the state to this provider, and requires a plan without replacements. Extend it when you touch a resource that
  existed in v0.18.0.

### Keep the provider thin

Neon's API decides behavior the user didn't configure. The provider maps configuration to API calls and API responses
to state.

- Don't invent defaults, sort orders, or fallbacks. Add provider-side behavior only for explicit configuration, a
  documented API contract, or a reproduced API, SDK, or mapping failure.
- Call the API through [`neon-sdk-go`](https://github.com/kislerdm/neon-sdk-go). Don't hand-roll HTTP requests or wrap
  the transport when the SDK has the operation. If the SDK lacks a field the API returns, track an SDK update instead
  of working around it.
- Bump the SDK in its own change, after checking the methods and models the provider uses.
- Wrap every API call in `projectReadiness.Retry` (diagnostics) or `projectReadiness.Do` (error). They retry while a
  project operation is still running and take per-status fallbacks, such as treating a 404 on delete as success.
- Don't map SDK zero values (`""`, `0`, `false`) to null unless the API documents that the zero value means "not set".

### Schema

- The model struct uses Framework types (`types.String`, `types.List`, ...) for schema fields only; internal helpers
  use Go types. Every `tfsdk` tag must match a schema attribute.
- Prefer a flat attribute to a nested object with one field: `cron = "0 * * * *"`, not `schedule = { cron = ... }`.
- Computed values that don't change after create get `UseStateForUnknown`, so plans don't show `(known after apply)`
  on every run.
- Mark secrets `Sensitive`. Prefer ephemeral resources or write-only arguments for values that shouldn't be in state
  at all; `neon_connection_uri` and the `neon_role` ephemeral resource replaced the old password attributes.
- Write descriptions for users: what the attribute does, its allowed values, and whether changing it replaces the
  resource. Don't describe how the provider calls the API.

### Validation

The API validates its input, so most values aren't checked at plan time. Validate in the provider when:

- the value is a type discriminator or enum the SDK exposes a constructor for, such as `neon.NewScheduleTriggerType`, or
- the Neon OpenAPI spec publishes the pattern and the API would reject the value only during apply, after other
  resources were created. Function slugs (`^[a-z0-9]{1,20}$`) are the current example.

Keep validator messages in the same words as the API's rule, and change them when the API rule changes.

### Errors

- Return API errors through `newDiagnostics`. It keeps Neon's message and request ID and adds a hint for the status
  code.
- Add a more specific hint only for a case Neon's message identifies, such as an organization API key on an operation
  that needs a personal key, or a branch without a read-write compute. Never drop or reword Neon's own message.
- In `Read`, a 404 means the resource was deleted outside Terraform: remove it from state so the next plan recreates
  it. In `Delete`, a 404 means it's already gone and counts as success. Anywhere else, a 404 is an error.

### Concurrency

Terraform creates and updates independent resources in parallel. When an API loses concurrent writes to the same
parent, serialize those writes in the provider. Neon Auth trusted domains and OAuth providers take a per-branch lock
(`lockNeonAuthWrites`) because Neon Auth dropped some of the writes that reached a branch at the same time. Add a test
that creates several sibling resources in one apply and checks that Neon has all of them.

### Data sources

- Reuse the resource's schema for returned objects with `newReadOnlyDataSourceAttrFromResourceAttr`, so field types,
  sensitivity, and descriptions stay in one place.
- Build the `id` from the configured scope, such as `<project_id>/<branch_id>`.
- A data source that returns a collection fetches every page. Read the continuation cursor from each response, treat
  it as opaque, and stop only on the signal the endpoint documents. Retry a failed page with the same cursor, and
  return nothing if a later page fails.

### Comments

Write a comment only for a constraint the code can't show: why a fallback exists, why an attribute is omitted, which
API behavior a workaround answers. Don't narrate the next line or restate a function's name.

## Testing

### Unit and plan-only tests

`make test` runs the unit tests. It inherits your environment, so unset `TF_ACC` first (`env -u TF_ACC make test`);
with `TF_ACC=1` exported, it also runs acceptance tests against Neon. Use `resource.UnitTest` with `PlanOnly` to test validation and plan
behavior without contacting Neon, and SDK stubs supplied through `Configure` for data source mapping. Don't assert
client headers, schema attribute counts, or type names.

### Acceptance tests

Acceptance tests create real Neon resources, so they take time and can cost money. They run with:

| Variable | Value |
| --- | --- |
| `TF_ACC` | `1` |
| `NEON_API_KEY` | An organization API key for the organization the tests create projects in |
| `NEON_PERSONAL_API_KEY` | A personal API key of a member of that organization, for the tests that manage API keys, members, and permissions |
| `ORG_ID` | The organization ID |
| `SCALE_PLAN` | `1` when the organization is on the Scale plan, to run the tests that need it |

```sh
TF_ACC=1 go test -tags=acceptance ./provider/ -run 'TestAccNeonTrigger' -v -timeout 60m
make testacc   # everything
```

Some acceptance test files carry `//go:build acceptance`; without `-tags=acceptance`, `go test` skips them silently.

- Tests name their projects with a prefix (`newProjectName`) and clean up what they created. Don't run two suites
  against the same organization at once; they can delete each other's projects.
- After a run, list the projects in the organization. Delete leftovers with [`cmd/cleanup`](cmd/cleanup/README.md).
- When a test fails, run it on the base commit before calling the failure pre-existing.

### What a change needs

| Change | Tests |
| --- | --- |
| New resource | Create and read, update or forced replacement, import, deletion outside Terraform, invalid input, and boundaries. Check Neon's state through the SDK, not only Terraform's state |
| New data source | SDK-stub tests for inputs, identity, mapping, and errors; a live acceptance test when the mapping depends on real API behavior |
| List data source | The five scenarios below, plus multiple pages and a failing later page when it paginates |
| Bug fix | A test that fails without the fix and passes with it |

List data source scenarios:

| Scope and API result | Expected outcome |
| --- | --- |
| Existing project, missing branch | Error |
| Missing project | Error |
| One item | One item |
| Two items | Two items |
| No items | Empty collection, checked with `length(...)` |

Inside `Check` functions, read the IDs Terraform created from `*terraform.State`, and use the SDK client for
independent assertions about Neon and for out-of-band changes in `PreConfig`. Keep HCL configs in file-scope helper
functions that take only the values that change between steps.

## Documentation

- Every resource has an example in `examples/` and a template in `templates/`, named without the `neon_` prefix:
  `templates/resources/branch.md.tmpl` renders `docs/resources/branch.md`.
- Regenerate `docs/` with `make docu`. The generator names the provider after the checkout directory, so run it from a
  checkout named `terraform-provider-neon`; in a directory with another name it fails and deletes `docs/`
  (`git checkout -- docs` restores it). CI fails when `docs/` doesn't match the generated output.
- Examples must pass `terraform validate` with the current provider. Use unique resource addresses and the current
  syntax; users copy examples whole.

## Changelog

Add an entry under `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md) for every change a user would notice, in the
`Added`, `Changed`, or `Fixed` section. Write it as the user sees it, in a sentence or two. Internal refactors and
test-only changes need no entry.

## Pull requests

- One concern per pull request: the fix, plus identical instances of the same bug.
- Title commits and pull requests as an imperative sentence: `Validate Function slugs at plan time`.
- Describe the problem, the user-facing change with an HCL or output example, and how you verified it.
- Maintainers treat a pull request from outside the team as a report of the problem it fixes. They reproduce the
  problem, write and test their own fix on a branch in this repository, credit you in that pull request, and close
  yours with a link to it. An issue with a reproduction is the fastest way to get a problem fixed.
- Pull requests from forks run the build, lint, docs, and unit tests. Acceptance tests need repository secrets and run
  only on branches in this repository.

Releases are cut by maintainers; [`.agents/skills/releasing-the-provider`](.agents/skills/releasing-the-provider/SKILL.md)
describes the process.

## Code of conduct

This project follows the [code of conduct](.github/CODE_OF_CONDUCT.md).
