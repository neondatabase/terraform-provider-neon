---
name: adding-neon-resources-and-data-sources
description: Use when adding a new `neon_*` resource or data source to the Neon Terraform provider.
---

# Adding Neon resources and data sources

Follow the approved plan and repository conventions.

| Before you | Read |
|---|---|
| Design schemas, call the SDK, or generate docs | [schema-and-api.md](schema-and-api.md) |
| Write or reformat tests | [testing.md](testing.md) |

## Workflow

1. Create branches from `main` or an approved base, make signed commits (`git commit -S`), push to the operator's fork, and open PRs only when the operator asks.
   Use `type(scope): message` with a few words, such as `feat(branch-snapshots): add data source`.
   Never comment on PRs.
2. Before code, write a plan the operator approves, with scope, design, requirements, and tasks.
   Record API and pinned SDK methods, identity, import, drift, errors, tests, changelog entries, docs command, and manual scenarios.
   Resolve API, SDK, permission, and compatibility questions before implementation.
3. Check the pinned SDK and OpenAPI for signatures, nullability, and errors.
   Do not bump the SDK without a method-level audit and version-gate decision.
   Wrap API calls in `projectReadiness.Do` or `projectReadiness.Retry` from `provider/retry.go`; document status-specific fallbacks.
4. Follow [schema-and-api.md](schema-and-api.md) and register the implementation in `provider/provider.go`.
   Keep unrelated refactors out.
5. Add the coverage in [testing.md](testing.md), including the complete resource acceptance floor in `provider/acc_<name>_test.go`.
   Add an operator-run Terraform scenario in the operator's external manual testing directory (ask if unknown), under `manual-<resource-name>/`.
   Include a README and self-contained scenarios covering prerequisites, commands, expected state, live API checks, and cleanup.
   Data-source scenarios include unset and configured inputs.
   Keep credentials, state, caches, plans, and provider overrides out of version control.
6. Edit examples, templates, and schema descriptions, never `docs/`.
   Run `make docu`.
   If `main.go` does not pass `--provider-name terraform-provider-neon` to tfplugindocs, run `go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name terraform-provider-neon` from the repository root instead, so worktrees keep the right page titles.
   Add `CHANGELOG.md` entries under `## [Unreleased]`; create it above the latest release if missing.
   Maintainers own version numbers and release sections.
7. Run approved verification and record the tested revision, results, and pending live or manual gates.
   Diagnose failures against the untouched base first.
   Do not restore legacy code for obsolete tests.

## Comments

- Add no new source comments without operator approval.
- Keep rationale in PR bodies or the approved design.
- Propose comments only for decisions the code cannot show, not framework wiring, signatures, or narration.

## Common mistakes

- Inventing API validation, defaults, or fallbacks.
- Copying entity schemas instead of deriving them.
- Shrinking required coverage or skipping manual scenarios.
- Treating fixture or historical results as current live evidence.
