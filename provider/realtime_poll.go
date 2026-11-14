package provider

import (
	"context"
	"time"
)

const (
	realtimeOperationBudget = 5 * time.Minute
	realtimePollInterval    = time.Second
)

func realtimeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, realtimeOperationBudget)
}

func realtimePoll(ctx context.Context, interval time.Duration, check func(context.Context) (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, err := check(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if done {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func realtimeAttempt(ctx context.Context, call func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := call()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
