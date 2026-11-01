package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	neon "github.com/kislerdm/neon-sdk-go"
)

func main() {
	client, err := neon.NewClient(neon.Config{Key: os.Getenv("NEON_API_KEY")})
	if err != nil {
		panic(err)
	}

	var cursorProjects *string
	protected := false
	for {
		p, _ := client.ListProjects(cursorProjects, nil, nil, nil, nil, nil)
		maxN := len(p.Projects)
		slog.Info("projects list", slog.Int("cnt", maxN))

		var flags = make(chan struct{}, maxN)
		for _, pr := range p.Projects {
			go func(projectID string) {
				var cursor *string
				for {
					br, err := client.ListProjectBranches(projectID, nil, nil, cursor, nil, nil,
						nil)
					if err != nil {
						slog.Error("error listing branches",
							slog.String("projectID", projectID),
							slog.String("error", err.Error()),
						)
					} else {
						slog.Info("list branches", slog.String("projectID", projectID),
							slog.Int("cnt", len(br.Branches)))
					}

					for _, b := range br.BranchesResponse.Branches {
						opResp, err := client.UpdateProjectBranch(projectID, b.ID, neon.BranchUpdateRequest{
							Branch: neon.BranchUpdateRequestBranch{Protected: &protected},
						})
						if err != nil {
							slog.Error("error setting branch to unprotected",
								slog.String("projectID", projectID),
								slog.String("branchID", b.ID),
								slog.String("error", err.Error()),
							)
						} else {
							waitUnfinishedOperations(context.Background(), client, opResp.OperationsResponse.Operations)
						}
					}

					cursor = br.CursorPaginationResponse.Pagination.Next
					if cursor == nil {
						break
					}
				}
				_, err := client.DeleteProject(projectID)
				if err != nil {
					slog.Error("error deleting project",
						slog.String("projectID", projectID),
						slog.String("error", err.Error()),
					)
				}

				flags <- struct{}{}
			}(pr.ID)
		}

		for maxN > 0 {
			<-flags
			maxN--
		}

		if p.PaginationResponse.Pagination == nil {
			break
		}
		cursorProjects = &p.PaginationResponse.Pagination.Cursor
		if cursorProjects == nil {
			break
		}
	}
}

func waitUnfinishedOperations(ctx context.Context, c *neon.Client, ops []neon.Operation) {
	var unfinishedOps = make([]neon.Operation, 0, len(ops))
	for _, op := range ops {
		if unfinishedOperation(op) {
			unfinishedOps = append(unfinishedOps, op)
		}
	}

	maxN := len(unfinishedOps)
	var flags = make(chan struct{}, maxN)

	for _, op := range unfinishedOps {
		go func(op neon.Operation) {
			var finished bool
			for !finished {
				slog.DebugContext(ctx, "wait for unfinished operation",
					slog.String("projectID", op.ProjectID),
					slog.String("operationID", op.ID),
				)
				time.Sleep(100 * time.Millisecond)
				resp, err := c.GetProjectOperation(op.ProjectID, op.ID)
				if err != nil {
					slog.ErrorContext(ctx, "error getting operation status",
						slog.String("projectID", op.ProjectID),
						slog.String("operationID", op.ID),
						slog.String("error", err.Error()),
					)
				} else {
					finished = !unfinishedOperation(resp.Operation)
				}
			}
			flags <- struct{}{}
		}(op)
	}

	for maxN > 0 {
		<-flags
		maxN--
	}
}

func unfinishedOperation(op neon.Operation) bool {
	var o bool
	switch op.Status {
	case neon.OperationStatusRunning, neon.OperationStatusScheduling:
		o = true
	}
	return o
}
