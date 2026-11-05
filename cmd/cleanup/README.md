# Scoped project cleanup

This command targets only project IDs you supply.
It never lists all projects or disables branch protection.
Set `NEON_API_KEY` in your environment before contacting Neon.

## Preview

```sh
go run ./cmd/cleanup --project-id PROJECT_ID --project-id OTHER_PROJECT_ID
```

Preview is read-only.
It reads each selected project and every page of its branches, then prints project IDs, names, and protection status.
Repeated IDs are deduplicated.
Missing IDs, paths, URLs, wildcard selectors, and comma-separated lists are rejected before creating the API client.
`--help` requires no credentials or API requests.

## Delete selected projects

Deleting a project removes its branches and data.
Review the preview and verify every ID before adding `--execute`:

```sh
go run ./cmd/cleanup --project-id PROJECT_ID --execute
```

Execution repeats the read-only preflight for every target before the first deletion.
Any protected branch, failed lookup, invalid response identity, missing branch list, or repeated pagination cursor aborts execution without deleting a project.
There is no unprotect option.

Deletes run sequentially.
A deletion failure stops remaining targets and returns a nonzero exit status.
Earlier successful deletes are not rolled back; inspect the reported IDs before retrying.
Preflight is a snapshot, not an atomic transaction with deletion.
Server-side authorization and protection checks still apply if another actor changes a project concurrently.

## Time limits

The total time limit defaults to two minutes; change it with `--timeout 5m`.
Each HTTP request also has a 30-second limit.
Interrupt or termination signals cancel the active request.
The command returns errors rather than retrying indefinitely or ignoring failed operations.

## Offline checks

```sh
go test -race -count=1 ./cmd/cleanup
```

Tests use the real SDK with mock HTTP only.
They do not access Neon or delete real projects.
