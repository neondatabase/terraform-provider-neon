# Tests for resources and data sources

Reference for [adding-neon-resources-and-data-sources](SKILL.md). The required coverage per change type is in
[CONTRIBUTING.md](../../../CONTRIBUTING.md#what-a-change-needs).

## Acceptance tests for a resource

Model new tests on `provider/acc_neon_snapshot_test.go`. One `Test<Resource>` function skips unless `TF_ACC=1` and
`ORG_ID` are set, creates an SDK client, and runs one `t.Run` per scenario:

| Scenario | Shape |
| --- | --- |
| Create and read | Apply the config, check attributes in state, then check the same values through the SDK |
| Update | Change one attribute; for in-place updates, assert `plancheck.ExpectResourceAction(..., plancheck.ResourceActionUpdate)` in `ConfigPlanChecks.PreApply` |
| Forced replacement | Assert `plancheck.ResourceActionReplace` for each attribute that forces it |
| Import | `ImportState: true`, `ImportStateVerify: true`, and the documented import ID |
| Deleted outside Terraform | Delete through the SDK in `PreConfig`, then `PlanOnly: true` with `ExpectNonEmptyPlan: true` |
| Invalid input | `ExpectError` with a regular expression for the error text |

```go
client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY"), HTTPClient: lockRetryingHTTPClient{}})
require.NoError(t, err)

t.Run("shall import a snapshot by <project_id>/<snapshot_id>", func(t *testing.T) {
	projectName := newProjectName("neonSnapshotAcc")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: newSnapshotConfig(projectName, orgID, "tf-acc-import", ""),
				Check:  verifySnapshotAgainstAPI(t, client, "tf-acc-import"),
			},
			{
				ResourceName:      "neon_snapshot.this",
				ImportState:       true,
				ImportStateVerify: true,
				// Builds <project_id>/<snapshot_id> from the state the first step wrote.
				ImportStateIdFunc: func(state *terraform.State) (string, error) {
					r := state.RootModule().Resources["neon_snapshot.this"].Primary
					return r.Attributes["project_id"] + "/" + r.Attributes["snapshot_id"], nil
				},
			},
		},
	})
})
```

- Create the SDK client with `lockRetryingHTTPClient`, so out-of-band calls wait while an earlier step's operation is
  still running.
- Name projects with `newProjectName(prefix)`; cleanup and leftover detection depend on the prefix.
- Read the IDs Terraform created from `*terraform.State` in `Check` functions. Use the SDK to assert what Neon has,
  not to find what Terraform created.
- Keep each config in a file-scope helper such as `newSnapshotConfig(projectName, orgID, name, expiresAt)` that takes
  only the values that change between steps.
- Tests that need a personal API key call `usePersonalAPIKey(t)` first.
- Tests that need the Scale plan skip unless `SCALE_PLAN=1`.
- When the API loses concurrent writes, add a scenario that creates several sibling resources in one apply (`count`)
  and checks that Neon has every one. See `TestAccNeonAuth`.

## Plan-only tests

Validation and plan behavior don't need Neon. Run them with `resource.UnitTest` and a placeholder API key, so they run
in `make test` and on fork pull requests:

```go
resource.UnitTest(t, resource.TestCase{
	ProtoV6ProviderFactories: newProviderFactories(),
	Steps: []resource.TestStep{
		{
			Config:      triggerScheduleConfig("test-project", "test-org", "sched", "my-fn", "*/5 * * * *"),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`must be 1 to 20 lowercase letters or digits`),
		},
	},
})
```

Set `NEON_API_KEY` to a placeholder with `t.Setenv`, as `TestTriggerConfigPlan` does, or set `api_key` in the test
config, as `TestFunctionSlugValidation` does.

## Data source tests

Data sources are tested with a fixture HTTP client passed to the SDK, as in `provider/data_source_branches_test.go`.
The fixture checks the request path and query and returns a recorded response:

- Use Terraform data source HCL as the input and assert the mapped attributes, the `id`, and the error for API failures.
- For a list data source, cover the five scenarios in CONTRIBUTING.md. For pagination, also cover two or more pages,
  a retried later page that isn't duplicated, and a failing later page that returns no partial result.
- Add a fixture case only for an outcome the API documents or one you reproduced. A synthetic response proves behavior
  only under its own assumptions.
- Don't assert `Authorization` or `User-Agent` headers, attribute counts, or type names, and don't repeat the shared
  retry tests in every suite.

## Readability

- One field per line in table cases and test steps.
- HCL and JSON fixtures as multiline strings.
- One case per scenario; don't list the remaining cases in a comment.
- Run `gofmt`, then read the diff once more.
