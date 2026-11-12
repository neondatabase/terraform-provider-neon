---
name: adding-neon-resources-and-data-sources
description: Use when adding a new `neon_*` resource or data source to the Neon Terraform provider or porting an SDK v2 resource to framework.
---

# Adding Neon resources and data sources

Follow the approved plan and repository conventions when adding provider features.
For SDK v2 migrations, also use the `migrating-neon-sdkv2-to-framework` skill.

| Before you | Read |
|---|---|
| Design schemas, call the SDK, paginate, or map errors | [schema-and-api.md](schema-and-api.md) |
| Write or reformat tests | [testing.md](testing.md) |

## Workflow

1. Create a feature branch off `main`.
   Sign every commit (`git commit -S`).
   Write commit messages as `type(subject): message`, such as `feat(branch-snapshots): add data source`.
2. Before code, document the approved scope, design, requirements, and implementation tasks.
   Include the API and pinned SDK methods, identity and import semantics, drift and error handling, test coverage, changelog ownership, generated documentation command, and manual-test path and scenarios.
   Record unresolved API, SDK, permission, and compatibility questions before implementation.
3. Check the pinned SDK and OpenAPI for signatures, nullability, and error behavior.
   Do not bump the SDK without a separate method-level audit and version-gate decision.
   Prefer existing SDK operations and repository retry helpers; document status-specific fallbacks.
4. Implement the resource or data source using current repository conventions and [schema-and-api.md](schema-and-api.md).
   Register it in `provider/provider.go` and keep the change focused.
   Do not refactor unrelated resources, even when asked in passing.
5. Add tests from [testing.md](testing.md).
   Add an independent operator-run manual Terraform scenario under `../../neon/testing/manual-<resource-name>/`, with a README and self-contained scenario directories.
   Cover prerequisites, commands, expected state, live API checks, and cleanup; data-source scenarios include unset and configured inputs.
   Keep credentials, state, caches, plans, and provider overrides out of version control.
6. Hand-write examples and templates under `examples/` and `templates/`.
   Name templates without the `neon_` prefix: `templates/data-sources/branch_snapshots.md.tmpl` renders `docs/data-sources/branch_snapshots.md`.
   Templates contain frontmatter, `{{ .Description }}`, `{{ tffile "..." }}`, and `{{ .SchemaMarkdown | trimspace }}`; never hand-write a Schema section.
   Write short user-facing descriptions such as "Fetch Branch Snapshots.", not how the provider calls the API or SDK.
   Generate registry docs with `make docu` if its directive supplies the explicit provider name; otherwise run `go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name terraform-provider-neon` from the repository root.
   Never edit rendered docs by hand.
   Follow the recorded changelog ownership rather than assuming agents should edit `CHANGELOG.md`.
7. Run the approved verification commands and record the tested revision, results, and any pending live or manual gates.
   Diagnose build or test failures against the untouched base first.
   Do not restore removed legacy code just to pass obsolete tests.

## Comments

- Add no source comments without operator approval, except comments produced by the documentation generation pipeline.
- Propose a comment only when it records a decision the code cannot show, such as why a fallback exists or why a schema omits an attribute.
- Never comment framework wiring (`Configure`, `ProviderData`, registration), restate signatures, or narrate the next line.

## Common mistakes

- Treating resources and data sources as having the same lifecycle, file layout, or live-test requirements.
- Adding client-side validators for API input rules or inventing provider defaults, sort orders, or fallbacks.
- Mapping SDK zero values to null without approval.
- Copying a data-source entity field map instead of deriving it from the canonical resource schema.
- Hand-rolling HTTP or wrapping transports to rewrite errors when the SDK has the operation.
- Assuming one API page is the full collection, treating an empty cursor as the end, or treating an opaque cursor as a boolean.
- Using SDK lookups to rediscover Terraform-created IDs rather than asserting remote state independently.
- Dropping or shrinking required tests because an earlier resource or data source shipped with fewer.
- Skipping the external manual scenario, recording fixture tests as live-test evidence, or accepting historical results as proof for the current binary.
- Generating docs without the explicit provider name, editing generated pages, or assuming changelog ownership.
