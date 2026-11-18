---
name: releasing-the-provider
description: Use when preparing or publishing a release of the Neon Terraform provider (neondatabase/neon on the Terraform Registry), including beta releases. Covers the release pull request, the version pins, tagging, what the release pipeline builds and signs, how the registry ingests the release, and how to verify it.
---

# Releasing the provider

Releases go to the Terraform Registry as `neondatabase/neon`. The registry ingests a GitHub release on this repository,
so a release is a version tag on `main` plus a GitHub release with signed assets. Maintainers run Neon's internal
release pipeline; contributors prepare the release pull request.

## 1. Release pull request

1. Pick the version: `vX.Y.Z`, or `vX.Y.Z-beta.N` while the provider is in beta.
2. In `CHANGELOG.md`, rename `## [Unreleased]` to `## [vX.Y.Z] - YYYY-MM-DD`. The pipeline takes the release notes from
   this section, matched by version.
3. Update every pinned version. Terraform installs a beta release only when `version` names it exactly, so the
   examples must match the release:

   ```sh
   grep -rn '<previous version>' README.md examples templates
   ```

   Today the pins are in `README.md`, `examples/provider/provider.tf`, `examples/e2e_example.tf`, and
   `templates/guides/migrate-from-kislerdm-neon.md.tmpl`.
4. Run `make docu` and commit the `docs/` changes.
5. Open the pull request as `Release vX.Y.Z`, wait for every check, including the acceptance tests on both Terraform
   versions, and merge it.

## 2. Tag

Wait for the post-merge CI run on `main` to pass, then put a lightweight tag on the merge commit:

```sh
gh api repos/neondatabase/terraform-provider-neon/git/refs \
  -f ref=refs/tags/vX.Y.Z -f sha=<merge commit sha>
```

The tag must point at a commit on `main`, and no branch may have the tag's name; the registry rejects a release whose
tag matches a branch.

## 3. Build, sign, and publish

A maintainer runs the release pipeline for the tag, first as a dry run and then for real. It:

- builds the provider for 13 operating system and architecture pairs with `-X main.version=<version>`, and zips each
  binary with `CHANGELOG.md`, `LICENSE`, and `README.md`
- writes `terraform-provider-neon_<version>_manifest.json` and `terraform-provider-neon_<version>_SHA256SUMS`
- scans the artifacts
- signs `SHA256SUMS` with the GPG key registered for the `neondatabase` registry namespace (key ID `922DFC48EB2F15FF`),
  producing `SHA256SUMS.sig`
- creates the GitHub release for the tag with those 16 assets and the CHANGELOG section as notes

`.goreleaser.yml` in this repository isn't used by the pipeline.

## 4. Verify

The registry ingests the GitHub release within seconds of publication:

```sh
curl -s https://registry.terraform.io/v1/providers/neondatabase/neon/versions | jq -r '.versions[].version'
```

Then install it the way users do, from an empty directory with `version = "X.Y.Z"` pinned:

```text
$ terraform init
- Installed neondatabase/neon vX.Y.Z (self-signed, key ID 922DFC48EB2F15FF)
```

Apply a configuration that exercises what the release changed, check the result in Neon, confirm the next plan is
empty, and destroy it. The [testing-against-neon](../testing-against-neon/SKILL.md) skill covers the steps.

## Rules

- Never delete, move, or replace a published version. Users' lock files record its checksums, and replacing the assets
  breaks `terraform init` for them. Fix forward with a new version.
- Don't release from a branch, an unmerged commit, or a dirty tree.
- After the release, update the version pinned in the Terraform pages on neon.com
  (`/docs/reference/terraform` and `/docs/reference/terraform-migrate`) and remove any known issue the release fixed.
