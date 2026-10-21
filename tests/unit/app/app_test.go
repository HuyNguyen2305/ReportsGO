package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/app"
	"reportsgo/internal/config"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		wantJSON bool
	}{
		{"dev writes text", config.EnvDev, false},
		{"prod writes JSON", config.EnvProd, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := app.NewLogger(config.Config{Env: tt.env, LogLevel: "info"}, &buf)

			logger.Info("hello", "key", "value")

			out := buf.String()
			require.Contains(t, out, "hello")
			require.Contains(t, out, "value")
			if tt.wantJSON {
				var rec map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &rec), "output: %s", out)
				require.Equal(t, "hello", rec["msg"])
				require.Equal(t, "value", rec["key"])
			} else {
				require.Error(t, json.Unmarshal([]byte(out), &map[string]any{}), "dev output should not be JSON")
				require.Contains(t, out, "key=value")
			}
		})
	}
}

func TestNewLoggerHonorsLevel(t *testing.T) {
	tests := []struct {
		level     string
		wantDebug bool
		wantInfo  bool
		wantWarn  bool
		wantError bool
	}{
		{"debug", true, true, true, true},
		{"info", false, true, true, true},
		{"warn", false, false, true, true},
		{"error", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			logger := app.NewLogger(config.Config{Env: config.EnvProd, LogLevel: tt.level}, &buf)

			logger.Debug("m-debug")
			logger.Info("m-info")
			logger.Warn("m-warn")
			logger.Error("m-error")

			out := buf.String()
			require.Equal(t, tt.wantDebug, bytes.Contains([]byte(out), []byte("m-debug")))
			require.Equal(t, tt.wantInfo, bytes.Contains([]byte(out), []byte("m-info")))
			require.Equal(t, tt.wantWarn, bytes.Contains([]byte(out), []byte("m-warn")))
			require.Equal(t, tt.wantError, bytes.Contains([]byte(out), []byte("m-error")))
		})
	}
}

func TestRunFailsFastWhenDatabaseIsUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	called := false
	cfg := config.Config{
		Env:                config.EnvDev,
		HTTPAddr:           "127.0.0.1:0",
		DatabaseURL:        "postgres://u:s3cret@127.0.0.1:1/db?sslmode=disable", // nothing listens on port 1
		DBMaxConns:         2,
		DBMinConns:         0,
		DBStatementTimeout: time.Second,
		ShutdownTimeout:    time.Second,
		LogLevel:           "info",
	}
	var logs bytes.Buffer

	start := time.Now()
	err := app.Run(ctx, cfg, app.NewLogger(cfg, &logs), app.Options{Ready: func(net.Addr) { called = true }})

	require.Error(t, err)
	require.ErrorContains(t, err, "open database")
	require.NotContains(t, err.Error(), "s3cret")
	require.Less(t, time.Since(start), 8*time.Second, "must fail promptly, not hang")
	require.False(t, called, "the server must not start listening without a database")
	require.NotContains(t, logs.String(), "starting")
}

func TestRunRejectsInvalidPoolSettings(t *testing.T) {
	cfg := config.Config{DatabaseURL: "postgres://u:p@127.0.0.1:1/db", DBMaxConns: 0, DBStatementTimeout: time.Second}

	err := app.Run(context.Background(), cfg, slog.New(slog.DiscardHandler), app.Options{})

	require.ErrorContains(t, err, "invalid pool config")
}
