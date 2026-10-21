package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	handlermocks "reportsgo/tests/mocks/handler"
)

func TestHealthz(t *testing.T) {
	// The pinger must never be called: liveness does not depend on the database.
	pinger := handlermocks.NewMockPinger(t)
	engine := handler.NewRouter(handler.Deps{Pinger: pinger})

	rec := do(engine, http.MethodGet, "/healthz", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got dto.HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, dto.HealthResponse{Status: "ok"}, got)
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		want       dto.ReadyResponse
	}{
		{
			name:       "database reachable",
			wantStatus: http.StatusOK,
			want:       dto.ReadyResponse{Status: "ready", Checks: map[string]string{"database": "ok"}},
		},
		{
			name:       "database down",
			pingErr:    errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			want:       dto.ReadyResponse{Status: "unavailable", Checks: map[string]string{"database": "down"}},
		},
		{
			name:       "ping timed out",
			pingErr:    context.DeadlineExceeded,
			wantStatus: http.StatusServiceUnavailable,
			want:       dto.ReadyResponse{Status: "unavailable", Checks: map[string]string{"database": "down"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinger := handlermocks.NewMockPinger(t)
			pinger.EXPECT().Ping(mock.Anything).Run(func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				require.True(t, ok, "ping must run with a deadline")
				require.WithinDuration(t, time.Now().Add(time.Second), deadline, 500*time.Millisecond)
			}).Return(tt.pingErr).Once()

			logger, logs := newLogger()
			engine := handler.NewRouter(handler.Deps{Logger: logger, Pinger: pinger, ReadyTimeout: time.Second})

			rec := do(engine, http.MethodGet, "/readyz", "")

			require.Equal(t, tt.wantStatus, rec.Code)
			var got dto.ReadyResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, tt.want, got)

			if tt.pingErr != nil {
				require.NotContains(t, rec.Body.String(), "10.0.0.5", "ping error text must not reach the client")
				require.Contains(t, logs.String(), "readiness check failed")
			}
		})
	}
}

func TestReadyzUsesDefaultTimeoutWhenUnset(t *testing.T) {
	pinger := handlermocks.NewMockPinger(t)
	pinger.EXPECT().Ping(mock.Anything).Run(func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(handler.DefaultReadyTimeout), deadline, 500*time.Millisecond)
	}).Return(nil).Once()

	rec := do(handler.NewRouter(handler.Deps{Pinger: pinger}), http.MethodGet, "/readyz", "")
	require.Equal(t, http.StatusOK, rec.Code)
}
