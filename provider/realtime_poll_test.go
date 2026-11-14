package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRealtimePoll(t *testing.T) {
	t.Run("immediate and pending", func(t *testing.T) {
		calls := 0
		start := time.Now()
		err := realtimePoll(context.Background(), time.Millisecond, func(context.Context) (bool, error) { calls++; return calls == 3, nil })
		require.NoError(t, err)
		require.Equal(t, 3, calls)
		require.GreaterOrEqual(t, time.Since(start), 2*time.Millisecond)
	})
	t.Run("one second schedule is cancellable", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		err := realtimePoll(ctx, realtimePollInterval, func(context.Context) (bool, error) { calls++; cancel(); return false, nil })
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, calls)
		require.Equal(t, time.Second, realtimePollInterval)
	})
	t.Run("deadline stops new attempts", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		calls := 0
		err := realtimePoll(ctx, time.Hour, func(context.Context) (bool, error) { calls++; return false, nil })
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, calls)
		err = realtimePoll(ctx, time.Millisecond, func(context.Context) (bool, error) { calls++; return true, nil })
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, calls)
	})
	t.Run("error and post call cancellation", func(t *testing.T) {
		sentinel := errors.New("SDK failure")
		require.ErrorIs(t, realtimePoll(context.Background(), time.Millisecond, func(context.Context) (bool, error) { return false, sentinel }), sentinel)
		ctx, cancel := context.WithCancel(context.Background())
		require.ErrorIs(t, realtimePoll(ctx, time.Millisecond, func(context.Context) (bool, error) { cancel(); return true, nil }), context.Canceled)
	})
	t.Run("operation budget capped by caller", func(t *testing.T) {
		ctx, cancel := realtimeContext(context.Background())
		defer cancel()
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(5*time.Minute), deadline, time.Second)
		earlier, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		bounded, stop := realtimeContext(earlier)
		defer stop()
		a, _ := earlier.Deadline()
		b, _ := bounded.Deadline()
		require.Equal(t, a, b)
	})
}
