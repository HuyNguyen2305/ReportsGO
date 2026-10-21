// Command api serves the Reports HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"reportsgo/internal/app"
	"reportsgo/internal/config"
)

func main() {
	os.Exit(run())
}

// run is separate from main so deferred cleanup runs before the process exits.
func run() int {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		// No logger exists yet, and the error never contains secrets.
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	logger := app.NewLogger(cfg, os.Stderr)
	slog.SetDefault(logger)
	gin.SetMode(gin.ReleaseMode) // Gin's debug output bypasses slog

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, logger, app.Options{}); err != nil {
		logger.Error("server failed", "error", err)
		return 1
	}
	return 0
}
