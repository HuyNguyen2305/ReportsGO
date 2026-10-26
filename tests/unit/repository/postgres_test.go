package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/repository"
)

func validPoolConfig() repository.PoolConfig {
	return repository.PoolConfig{
		URL:              "postgres://user:pw@localhost:5432/db",
		MaxConns:         10,
		MinConns:         2,
		StatementTimeout: 5 * time.Second,
	}
}

func TestPoolConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*repository.PoolConfig)
		wantErr string
	}{
		{"valid", func(*repository.PoolConfig) {}, ""},
		{"zero min conns is valid", func(c *repository.PoolConfig) { c.MinConns = 0 }, ""},
		{"missing url", func(c *repository.PoolConfig) { c.URL = "" }, "URL is required"},
		{"zero max conns", func(c *repository.PoolConfig) { c.MaxConns = 0; c.MinConns = 0 }, "MaxConns must be at least 1"},
		{"negative min conns", func(c *repository.PoolConfig) { c.MinConns = -1 }, "MinConns must be between 0 and MaxConns"},
		{"min above max", func(c *repository.PoolConfig) { c.MinConns = 11 }, "MinConns must be between 0 and MaxConns"},
		{"zero statement timeout", func(c *repository.PoolConfig) { c.StatementTimeout = 0 }, "StatementTimeout must be positive"},
		{"zero connect timeout means the default", func(c *repository.PoolConfig) { c.ConnectTimeout = 0 }, ""},
		{"negative connect timeout", func(c *repository.PoolConfig) { c.ConnectTimeout = -time.Second }, "ConnectTimeout must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validPoolConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNewPoolRejectsBadInputWithoutConnecting(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		cfg := validPoolConfig()
		cfg.MaxConns = 0
		_, err := repository.NewPool(context.Background(), cfg)
		require.ErrorContains(t, err, "invalid pool config")
	})

	t.Run("unparseable url", func(t *testing.T) {
		cfg := validPoolConfig()
		cfg.URL = "postgres://user:pw@host:notaport/db"
		_, err := repository.NewPool(context.Background(), cfg)
		require.ErrorContains(t, err, "parse database url")
		require.NotContains(t, err.Error(), "pw@")
	})
}
