# Test rules

Reference for `adding-neon-resources-and-data-sources`.

## Required coverage

| Type | Required coverage |
|---|---|
| Resource | Create/read, update or forced replacement, import, out-of-band deletion, invalid inputs and relevant boundaries, cleanup and partial failures where applicable. Compare Terraform state with independently observed Neon API state. A create-only smoke test is never enough. |
| Data source | Default to SDK-stub or recorded-fixture tests in `provider/data_source_<name>_test.go`, exercising `Configure`, inputs, identity, mapping, and SDK errors. Document the reason for any live acceptance test in the approved plan. |
| List data source | The five scenarios below. For pagination, also cover multiple pages, retry of a later page without duplicates, and later-page failure without partial success. |

Do not cut coverage because an earlier resource or data source shipped with less.

## List data-source scenarios

| Configured scope and API result | Expected outcome |
|---|---|
| Existing project, non-existing branch | Error |
| Non-existing project | Error |
| Existing project and branch with one item | One item in the collection |
| Existing project and branch with two items | Two items in the collection |
| Existing project and branch with no items | Empty collection, checked through `length(...)` |

- Keep the missing-project and missing-branch cases separate so each scope reaches the expected SDK operation.
- Use Terraform data-source HCL as each case's input and assert mapped fields, identity, and API errors.
- These counts describe fixture responses, not every live project's contents.

## Fixtures

- Supply the fixture SDK client through `Configure`.
- Do not assert `Authorization`, `User-Agent`, or other client headers, inspect client internals, or replace `http.DefaultTransport`; provider tests own client setup.
- A synthetic fixture proves behavior only under its own assumptions.
  Add a case, such as duplicate items across pages, only for a documented API outcome or a reproduced failure.
  Prefer extra assertions in an existing scenario over a new edge-case matrix.
- Do not test `Metadata()`, type names, schema versions, fixed attribute counts, or registration inventories per type.
- Do not duplicate shared retry or converter matrices in each data-source suite.

## Acceptance tests

- Read nearby tests for naming and layout.
- Use `newProviderFactories` and file-scope HCL helpers.
- Use Terraform state for created IDs and the SDK for independent remote assertions and cleanup.
- Use `ORG_ID` for the organization in code, prechecks, and skip messages; set it in approved test command environments.
  Never add resource-specific or `NEON_`-prefixed organization variables.
- For manual Terraform, set `TF_VAR_org_id` from the selected `ORG_ID`.

## Readability

- Expand every nontrivial table case and lifecycle step with one field per line.
- Put nested check calls and their arguments on separate lines.
- Format JSON and HCL fixtures as multiline documents.
- Write each scenario as its own case; do not list remaining cases in a comment.
- Formatting-only changes keep every scenario and assertion.
- Run `gofmt`, then review the diff for readability.
