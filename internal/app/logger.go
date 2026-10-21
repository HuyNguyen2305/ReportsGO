// Package app wires the configured dependencies into a running HTTP server.
package app

import (
	"io"
	"log/slog"

	"reportsgo/internal/config"
)

// NewLogger builds the process logger: human-readable text in dev, JSON in prod, at the configured level.
func NewLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.SlogLevel()}
	if cfg.Env == config.EnvProd {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
