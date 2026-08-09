// Command worker runs asynchronous chain-indexing and certificate jobs.
// Domain consumers are added incrementally; the process lifecycle is ready now.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("worker_started")
	<-ctx.Done()
	logger.Info("worker_stopped")
}
