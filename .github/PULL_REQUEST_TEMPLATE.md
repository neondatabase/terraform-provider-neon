   ## Changes
   <!-- Summary -->

   ## Tests

   - [ ] `go build ./...` exit 0
   - [ ] `go vet ./...` exit 0
   - [ ] `TF_ACC=1 NEON_API_KEY=... go test ./internal/provider -run
 TestAccAPIKeyFrameworkFSM -v` passes locally
   - [ ] `openspec validate framework-migration-pilot --strict` passes
   - [ ] covered with acceptance tests in `internal/provider/`
   - [ ] relevant change in `docs/` folder (if docs touched)
   - [ ] using Go SDK (SDK v2 resource preserved)
   - [ ] using TF Plugin Framework (new `internal/provider/` resource)
   - [ ] has entry in `CHANGELOG.md`
