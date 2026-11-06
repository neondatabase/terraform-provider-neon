package provider

import (
	"context"
	"sync"
	"testing"
	"time"

	neon "github.com/kislerdm/neon-sdk-go"
	"github.com/stretchr/testify/assert"
)

type mockOpsReader struct {
	rec         map[string][]time.Time
	maxRequests map[string]int
	mu          *sync.Mutex
}

func (m mockOpsReader) GetProjectOperation(_ string, operationID string) (o neon.OperationResponse, err error) {
	o = neon.OperationResponse{Operation: neon.Operation{
		ID:     operationID,
		Status: neon.OperationStatusFinished,
	}}
	m.mu.Lock()
	m.rec[operationID] = append(m.rec[operationID], time.Now())
	if m.maxRequests[operationID] > 0 {
		o.Operation.Status = neon.OperationStatusRunning
		m.maxRequests[operationID]--
	}
	m.mu.Unlock()
	return o, nil
}

func Test_waitUnfinishedOperations(t *testing.T) {
	t.Run("shall wait for two unfinished operations", func(t *testing.T) {
		operations := []neon.Operation{
			{
				ID:     "0",
				Status: neon.OperationStatusFinished,
			},
			{
				ID:     "1",
				Status: neon.OperationStatusRunning,
			},
			{
				ID:     "2",
				Status: neon.OperationStatusScheduling,
			},
		}

		reader := mockOpsReader{
			rec: make(map[string][]time.Time),
			mu:  new(sync.Mutex),
			maxRequests: map[string]int{
				"0": 0,
				"1": 1,
				"2": 1,
			},
		}
		assert.NoError(t, waitUnfinishedOperations(context.TODO(), reader, operations))
		assert.Nil(t, reader.rec["0"])
		for _, op := range operations[1:] {
			assert.Len(t, reader.rec[op.ID], 2)
			gotDelay := reader.rec[op.ID][1].Sub(reader.rec[op.ID][0])
			assert.GreaterOrEqual(t, gotDelay, operationsCompletionDelay)
		}
	})
	t.Run("shall not wait on empty operations", func(t *testing.T) {
		var operations []neon.Operation
		reader := mockOpsReader{
			rec: make(map[string][]time.Time),
		}
		assert.NoError(t, waitUnfinishedOperations(context.TODO(), reader, operations))
		assert.Empty(t, reader.rec)
	})
	t.Run("shall return an error for a failed operation", func(t *testing.T) {
		reason := "compute failed to start"
		operations := []neon.Operation{{
			ID:        "op-failed",
			ProjectID: "project",
			Action:    neon.OperationActionStartCompute,
			Status:    neon.OperationStatusFailed,
			Error:     &reason,
		}}
		err := waitUnfinishedOperations(context.TODO(), failingOpsReader{}, operations)
		assert.ErrorContains(t, err, `op-failed (start_compute) on Neon project project ended with status "failed": `+reason)
	})
	t.Run("shall stop waiting when the context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		operations := []neon.Operation{{ID: "op", ProjectID: "project", Status: neon.OperationStatusRunning}}
		err := waitUnfinishedOperations(ctx, failingOpsReader{}, operations)
		assert.ErrorIs(t, err, context.Canceled)
	})
	t.Run("shall give up after repeated status lookup failures", func(t *testing.T) {
		operations := []neon.Operation{{ID: "op", ProjectID: "project", Status: neon.OperationStatusRunning}}
		err := waitUnfinishedOperations(context.TODO(), failingOpsReader{}, operations)
		assert.ErrorContains(t, err, "unable to read the status of Neon operation op")
	})
}

type failingOpsReader struct{}

func (failingOpsReader) GetProjectOperation(_ string, _ string) (neon.OperationResponse, error) {
	return neon.OperationResponse{}, neon.Error{HTTPCode: 500}
}
