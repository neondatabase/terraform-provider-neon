# Schema, SDK, and error rules

Reference for `adding-neon-resources-and-data-sources`.
Record any exception, with its evidence and approval, in the approved plan.

## Schema

- Use Framework types for schema-facing model fields; use Go types for internal helpers.
- Give computed fields stable API semantics and a user-facing purpose.
  Omit transient or constant fields with no actionable value.
- Prefer flat attributes for single-field blocks.
- Use Terraform types and required fields for configuration structure; the server validates API values, including enum, regex, length, and numeric bounds.
- Compose a data source `id` from the real configured scope, such as `<project_id>/<branch_id>`, with no type suffix.
  Terraform tells instances apart by address, so another data source with the same scope ID does not collide.
  Test any change to an existing ID instead of claiming compatibility.
- Reuse canonical resource schemas, models, mappers, and existing read-only converters such as `newReadOnlyDataSourceAttrFromResourceAttr` for returned data-source entities.
  Preserve field types, sensitivity, descriptions, and deprecation metadata; strip defaults, validators, required or optional flags, and plan modifiers.
- Keep lookup and filter inputs at the data-source boundary and returned fields computed-only.
- Match every model `tfsdk` tag to a schema attribute exactly.
  Framework rejects a tagged model field with no attribute, even when nothing reads it.
  Sharing a model changes the public object shape, so review added and removed fields explicitly.
- Put field descriptions and units in the canonical resource schema so every consumer inherits the fix.
  Check the unit against the SDK and mapper first; never patch it in a data-source description override.
- Fetch collections with the bulk list operation; do not call a single-entity GET per item.
- Use typed Go slices for computed lists when Framework reflection can map them and null or unknown values need not be preserved.
- Initialize the slice when the contract needs an empty list instead of null, and test the difference through `length(...)`.

## SDK and API

- Keep the provider thin; the API server decides unconfigured behavior and defaults.
  Add provider-side decisions only for explicit user configuration, an applicable documented contract, or a reproduced API, SDK, or mapping failure.
- Do not assume generated SDK models keep nullable API fields.
  When a model drops a null (for example `time.Time` instead of `*time.Time`), record the gap as an open question and track an SDK fix.
- Map SDK values to Terraform null only when that mapping is explicitly approved; then use Framework pointer-value helpers to tell nil from an explicit empty string, zero, or false.
  Never mix unrelated fallback-to-null changes, such as `name` from `""` to null, into the same work.
- Do not hand-roll HTTP when the pinned SDK exposes the operation.
  "The SDK is flaky" is not evidence; a reproduced failure is.

## Pagination

- Fetch all cursor pages when the data source promises a collection; a single-page limit needs an approved exception.
- Read continuation metadata from the current page.
  End only on the signal the endpoint contract defines, such as a nil `Pagination`; a present empty cursor is not the end unless that contract says so.
- Treat cursors as opaque; preserve missing versus present values and encode them exactly once as the SDK requires.
- Set sorting only for user configuration, applicable SDK or API guidance, or an approved ordering contract.
  Do not resend a sort value because the response echoed it, and do not invent a fixed default.
  Send the same values on every page and retry.
- Retry a page with the same cursor and append it only after success.
  Reset collected results when a whole-list attempt restarts, and publish no partial state when a later page fails.
- For configured filters, document the SDK search semantics (substring, prefix, exact) and unset, null, and empty behavior.
  Keep the configured value in state as written and send it on every page and retry.
  Keep filter inputs distinct from fields inside the returned collection.

## Errors

- Preserve SDK diagnostics through repository helpers such as `newDiagnostics`; do not rewrite server messages.
- Do not add HTTP wrappers or `http.RoundTripper` layers that read bodies, inject fields, or replace server errors with provider wording.
- Treat HTTP 404 as not found; do not infer its cause or make another request to classify it.
- When the SDK discards an error field (such as a 403 `reason`), track an upstream SDK fix instead of a workaround.
  Expose the field only after an SDK version-gate decision.
- The provider still owns:
  - configuration structure errors
  - a missing or misconfigured provider client
  - transport failures
  - established retry handling
  - drift handling
