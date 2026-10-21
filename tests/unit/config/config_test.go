package config_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/config"
)

func lookupFrom(env map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

const validURL = "postgres://user:s3cret@localhost:5432/Reports?sslmode=disable"

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{"DATABASE_URL": validURL}))
	require.NoError(t, err)

	require.Equal(t, config.Config{
		Env:                config.EnvDev,
		HTTPAddr:           ":8080",
		DatabaseURL:        validURL,
		DBMaxConns:         10,
		DBMinConns:         2,
		DBStatementTimeout: 5 * time.Second,
		ShutdownTimeout:    15 * time.Second,
		CORSAllowedOrigins: nil,
		LogLevel:           "info",
	}, cfg)
}

func TestLoadParsesEveryVariable(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{
		"APP_ENV":              "prod",
		"HTTP_ADDR":            "127.0.0.1:9000",
		"DATABASE_URL":         validURL,
		"DB_MAX_CONNS":         "25",
		"DB_MIN_CONNS":         "0",
		"DB_STATEMENT_TIMEOUT": "750ms",
		"SHUTDOWN_TIMEOUT":     "1m",
		"CORS_ALLOWED_ORIGINS": " https://a.example , https://b.example,, ",
		"LOG_LEVEL":            "debug",
	}))
	require.NoError(t, err)

	require.Equal(t, config.EnvProd, cfg.Env)
	require.Equal(t, "127.0.0.1:9000", cfg.HTTPAddr)
	require.Equal(t, int32(25), cfg.DBMaxConns)
	require.Equal(t, int32(0), cfg.DBMinConns)
	require.Equal(t, 750*time.Millisecond, cfg.DBStatementTimeout)
	require.Equal(t, time.Minute, cfg.ShutdownTimeout)
	require.Equal(t, []string{"https://a.example", "https://b.example"}, cfg.CORSAllowedOrigins)
	require.Equal(t, slog.LevelDebug, cfg.SlogLevel())
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"missing database url", map[string]string{}, "DATABASE_URL: is required"},
		{"blank database url", map[string]string{"DATABASE_URL": "   "}, "DATABASE_URL: is required"},
		{"unparseable database url", map[string]string{"DATABASE_URL": "postgres://us er:pw@host:bad"}, "DATABASE_URL: not a valid URL"},
		{"bad env", map[string]string{"DATABASE_URL": validURL, "APP_ENV": "staging"}, "APP_ENV: must be one of dev, prod"},
		{"non-numeric max conns", map[string]string{"DATABASE_URL": validURL, "DB_MAX_CONNS": "many"}, "DB_MAX_CONNS: must be a positive integer"},
		{"zero max conns", map[string]string{"DATABASE_URL": validURL, "DB_MAX_CONNS": "0"}, "DB_MAX_CONNS: must be a positive integer"},
		{"negative min conns", map[string]string{"DATABASE_URL": validURL, "DB_MIN_CONNS": "-1"}, "DB_MIN_CONNS: must be a non-negative integer"},
		{"min above max", map[string]string{"DATABASE_URL": validURL, "DB_MAX_CONNS": "3", "DB_MIN_CONNS": "4"}, "DB_MIN_CONNS: must not exceed DB_MAX_CONNS"},
		{"bad statement timeout", map[string]string{"DATABASE_URL": validURL, "DB_STATEMENT_TIMEOUT": "soon"}, "DB_STATEMENT_TIMEOUT: must be a positive duration"},
		{"zero shutdown timeout", map[string]string{"DATABASE_URL": validURL, "SHUTDOWN_TIMEOUT": "0s"}, "SHUTDOWN_TIMEOUT: must be a positive duration"},
		{"bad log level", map[string]string{"DATABASE_URL": validURL, "LOG_LEVEL": "trace"}, "LOG_LEVEL: must be one of debug, info, warn, error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(lookupFrom(tt.env))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoadReportsAllErrorsTogether(t *testing.T) {
	_, err := config.Load(lookupFrom(map[string]string{
		"APP_ENV":      "nope",
		"DB_MAX_CONNS": "x",
		"LOG_LEVEL":    "loud",
	}))
	require.ErrorContains(t, err, "DATABASE_URL: is required")
	require.ErrorContains(t, err, "APP_ENV")
	require.ErrorContains(t, err, "DB_MAX_CONNS")
	require.ErrorContains(t, err, "LOG_LEVEL")
}

func TestLoadErrorNeverLeaksPassword(t *testing.T) {
	_, err := config.Load(lookupFrom(map[string]string{"DATABASE_URL": "postgres://user:s3cret@ho st:5432/db"}))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "s3cret")
}

func TestSlogLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, want, config.Config{LogLevel: name}.SlogLevel())
		})
	}
}

func TestLogValueRedactsPassword(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{"DATABASE_URL": validURL}))
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("config", "config", cfg)

	out := buf.String()
	require.NotContains(t, out, "s3cret")
	require.Contains(t, out, "xxxxx")
	require.Contains(t, out, "localhost:5432/Reports")
}
