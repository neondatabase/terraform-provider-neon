---
name: testing-against-neon
description: Use when verifying a provider change against the real Neon API, before calling it done or reviewing someone else's change. Covers API keys for acceptance tests, running a local build in a real configuration, the upgrade check from kislerdm/neon v0.18.0, CI behavior for forks and the shared acceptance queue, and cleanup.
---

# Testing against Neon

Unit tests and `go build` don't show state decoding, plan modifiers, or API behavior. Every change gets a live check:
the acceptance tests it touches, plus a local build in a real configuration when it changes what users see.

## API keys

Use throwaway keys for the run and revoke them afterwards. With the [Neon CLI](https://neon.com/docs/reference/neon-cli):

```sh
neon api-keys create --name tf-acc-$(date +%s) --org-id "$ORG_ID" --output json   # NEON_API_KEY
neon api-keys create --name tf-acc-personal-$(date +%s) --output json            # NEON_PERSONAL_API_KEY
neon api-keys revoke <key-id> --org-id "$ORG_ID"                                   # afterwards, for each key
```

Run acceptance tests on the organization key. With a personal key, a project without `org_id` lands in your default
organization, and only the tests that call `usePersonalAPIKey` need that key. The environment variables are listed in
[CONTRIBUTING.md](../../../CONTRIBUTING.md#acceptance-tests).

## Acceptance tests

```sh
TF_ACC=1 go test -tags=acceptance ./provider/ -run 'TestAccNeonSnapshot|TestBucket' -v -timeout 60m
```

- Run one suite per organization at a time. Tests clean up by project name prefix and can delete each other's projects.
- When a test fails, run it on the base commit (`git stash`, run, `git stash pop`) before calling the failure
  pre-existing. Compare the set of tests that ran, not only the failure count: a panic aborts the whole binary.
- A test that fails only sometimes is a finding. Reproduce the API behavior directly, with the SDK or `curl`, before
  deciding whether the provider or the test is wrong. `TestAccNeonAuth` failed intermittently until concurrent API calls
  showed that Neon Auth loses concurrent writes.

## A local build in a real configuration

Build the provider and point Terraform at it with `dev_overrides`, as described in
[CONTRIBUTING.md](../../../CONTRIBUTING.md#build-and-run-a-local-build). Then use it the way a user would, from a
scratch directory outside the repository:

1. Write the configuration a user would write, from the docs and examples, with `org_id` set.
2. `terraform apply`, then check the result in Neon: the Console, `neon` CLI, or API.
3. `terraform plan -detailed-exitcode` must exit 0. Exit code 2 after an apply means drift or a perpetual diff.
4. Change attributes that should update in place and confirm the plan says `update in-place`, not `must be replaced`.
5. Remove a resource from state and import it with an `import` block; the plan must show only the import.
6. `terraform destroy`, then confirm Neon has nothing left.

## Upgrade check from kislerdm/neon

Run this for any change to a resource that existed in `kislerdm/neon` v0.18.0: `neon_project`, `neon_branch`,
`neon_endpoint`, `neon_role`, `neon_database`, `neon_jwks_url`, `neon_api_key`, `neon_org_api_key`,
`neon_project_permission`, `neon_branch_backup_schedule`, and the VPC endpoint resources.

1. In one directory, write a configuration with `source = "kislerdm/neon"` and `version = "0.18.0"`, without the dev
   override, and `terraform apply` it.
2. Copy `terraform.tfstate` to a second directory with the same resources in this provider's syntax. The
   [migration guide](../../../docs/guides/migrate-from-kislerdm-neon.md) lists the changes.
3. Run there:

   ```sh
   terraform state replace-provider -auto-approve registry.terraform.io/kislerdm/neon registry.terraform.io/neondatabase/neon
   TF_CLI_CONFIG_FILE=/path/to/dev.tfrc terraform plan -out=plan.out
   terraform show -json plan.out | jq -r '.resource_changes[] | select(.change.actions | index("delete")) | .address'
   ```

   The last command must print nothing: no replacements or deletions. The plan should show no changes, or only
   in-place updates.
4. Destroy with the local build.

`TestMigrationFromKislerdm` automates a smaller version of this check in the acceptance suite.

## CI

- Pull requests run build, lint, docs generation, and unit tests. Acceptance tests run on Terraform 1.5 and 1.17 for
  branches in this repository; fork pull requests don't get the secrets, so the maintainer reviewing them runs them.
- Every acceptance job shares one concurrency group, and GitHub keeps only one pending job per group. A newer job,
  such as the post-merge run on `main`, cancels a queued one. A job that ended `cancelled` didn't fail: rerun it with
  `gh run rerun <run-id> --failed` once the queue is empty.

## Cleanup

After every run, list the projects in the test organization and revoke the keys you created. Delete leftovers by ID
with [`cmd/cleanup`](../../../cmd/cleanup/README.md), which previews before it deletes. A project with protected
branches needs the protection removed first.
