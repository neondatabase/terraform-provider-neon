package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	neon "github.com/kislerdm/neon-sdk-go"
)

type opsReader interface {
	GetProjectOperation(projectID string, operationID string) (neon.OperationResponse, error)
}

const (
	operationsCompletionDelay = 100 * time.Millisecond
	// maxOperationLookupFailures bounds consecutive failed status lookups for one operation.
	maxOperationLookupFailures = 10
)

// waitUnfinishedOperations blocks until every operation finishes, and returns an error
// if one of them fails, is cancelled, or its status cannot be read.
func waitUnfinishedOperations(ctx context.Context, c opsReader, ops []neon.Operation) error {
	for _, op := range ops {
		if err := waitOperation(ctx, c, op); err != nil {
			return err
		}
	}
	return nil
}

func waitOperation(ctx context.Context, c opsReader, op neon.Operation) error {
	failures := 0
	for {
		switch op.Status {
		case neon.OperationStatusFinished, neon.OperationStatusSkipped:
			return nil
		case neon.OperationStatusFailed, neon.OperationStatusError, neon.OperationStatusCancelled:
			return operationError(op)
		}

		tflog.Trace(ctx, "wait for unfinished operation", map[string]interface{}{
			"projectID":   op.ProjectID,
			"operationID": op.ID,
			"status":      op.Status.String(),
		})
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting for Neon operation %s (%s) on project %s: %w",
				op.ID, op.Action.String(), op.ProjectID, ctx.Err())
		case <-time.After(operationsCompletionDelay):
		}

		resp, err := c.GetProjectOperation(op.ProjectID, op.ID)
		if err != nil {
			failures++
			tflog.Warn(ctx, "error getting operation status", map[string]interface{}{
				"projectID":   op.ProjectID,
				"operationID": op.ID,
				"error":       err,
			})
			if failures >= maxOperationLookupFailures {
				return fmt.Errorf("unable to read the status of Neon operation %s (%s) on project %s: %w",
					op.ID, op.Action.String(), op.ProjectID, err)
			}
			continue
		}
		failures = 0
		op = resp.Operation
	}
}

func operationError(op neon.Operation) error {
	reason := ""
	if op.Error != nil && *op.Error != "" {
		reason = ": " + *op.Error
	}
	return fmt.Errorf("operation %s (%s) on Neon project %s ended with status %q%s. "+
		"Check the project's operations in the Neon Console, then run Terraform again",
		op.ID, op.Action.String(), op.ProjectID, op.Status.String(), reason)
}
