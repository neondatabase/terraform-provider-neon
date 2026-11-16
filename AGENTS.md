# AGENTS.md

Instructions for AI coding agents working on the Neon Terraform provider. [CONTRIBUTING.md](CONTRIBUTING.md) is the
full guide; read it before changing code. This file is the short version, plus what agents most often get wrong.

## Skills

Load the skill that matches the task before starting it:

| Task | Skill |
| --- | --- |
| Add a resource or data source, or change a schema | [adding-neon-resources-and-data-sources](.agents/skills/adding-neon-resources-and-data-sources/SKILL.md) |
| Verify a change against Neon: acceptance tests, a local build, the upgrade from `kislerdm/neon` | [testing-against-neon](.agents/skills/testing-against-neon/SKILL.md) |
| Prepare and publish a release | [releasing-the-provider](.agents/skills/releasing-the-provider/SKILL.md) |

## Rules

- **Existing users first.** Users run this provider against production projects. A change that makes an existing
  configuration plan a replacement or fail is a regression. Changing an attribute's type or nesting needs a schema
  `Version` bump and an `UpgradeState` entry.
- **Verify against the real API.** Unit tests and `go build` don't show state decoding, plan modifiers, or API
  behavior. Run the affected acceptance tests, or a local build against a real configuration, before calling a change
  done.
- **Regression tests fail first.** For a bug fix, show the test failing without the fix, then passing with it.
- **The SDK is the API client.** Use `neon-sdk-go` operations; don't write HTTP calls or transport wrappers. Don't bump
  the SDK as part of an unrelated change.
- **The API owns defaults and validation.** Don't invent defaults, sort orders, fallbacks, or validators. The two
  exceptions are SDK enum constructors and patterns the OpenAPI spec publishes when the API would reject the value only
  during apply (see CONTRIBUTING.md).
- **Keep Neon's error messages.** Return errors through `newDiagnostics`. Add a hint only for a case Neon's message
  identifies.
- **Generated docs are generated.** Never edit `docs/`; run `make docu` and commit the result.
- **One acceptance run per organization.** Tests clean up by project name prefix, so two suites against the same
  organization delete each other's projects.
- **Secrets stay out of the repository.** API keys, Terraform state, plans, and `dev.tfrc` files hold credentials or
  local paths.

## Before you call a change done

```sh
gofmt -l provider/            # prints nothing
go vet ./...
make lint
make test
TF_ACC=1 go test -tags=acceptance ./provider/ -run '<the tests your change touches>' -v
make docu                     # commit the docs/ diff
```

Also add a `CHANGELOG.md` entry under `[Unreleased]` for anything a user would notice, and make sure every example you
touched passes `terraform validate`.

## Facts agents get wrong

- The test provider factory is `newProviderFactories()`, in `provider/provider.go`.
- API calls go through `projectReadiness.Retry` (returns diagnostics) or `projectReadiness.Do` (returns an error), in
  `provider/retry.go`. There is no `RetryFramework`.
- Neon refuses organization API keys for API keys, organization and project members, project permissions, and
  `GET /regions`. Tests for those call `usePersonalAPIKey(t)`, which switches to `NEON_PERSONAL_API_KEY`; everything
  else runs on the organization key.
- Neon creates roles and databases through a branch's read-write compute. A role on a branch whose compute the same
  configuration creates needs `depends_on = [neon_endpoint.<name>]`.
- Terraform opens ephemeral resources during plan once their arguments are known. An ephemeral resource can't read a
  role that the same run creates in an existing project.
- Neon Auth loses concurrent writes on a branch. Writes to its trusted domains and OAuth providers take
  `lockNeonAuthWrites`; new Neon Auth write paths need it too.
- Terraform installs a beta release only when `version` names it exactly. Examples and guides pin the current beta.
