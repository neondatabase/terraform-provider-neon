package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	neon "github.com/kislerdm/neon-sdk-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, newCleanupClient); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCleanupClient(ctx context.Context) (*neon.Client, error) {
	return neon.NewClient(neon.Config{
		Key: os.Getenv("NEON_API_KEY"),
		HTTPClient: &cliBoundHTTP{
			ctx:  ctx,
			base: &http.Client{Timeout: 30 * time.Second},
		},
	})
}
