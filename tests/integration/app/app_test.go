//go:build integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/app"
	"reportsgo/internal/config"
	"reportsgo/internal/dto"
	"reportsgo/internal/handler"
	"reportsgo/tests/integration/testdb"
)

func init() { gin.SetMode(gin.TestMode) }

const allowedOrigin = "https://app.example.com"

// syncBuffer is a goroutine-safe log sink: the server writes while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records parses the JSON log lines written so far.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "log line is not JSON: %s", line)
		out = append(out, rec)
	}
	return out
}

type registrarFunc func(rg *gin.RouterGroup)

func (f registrarFunc) Register(rg *gin.RouterGroup) { f(rg) }

// testServer is a running app.Run on a random port.
type testServer struct {
	base string
	logs *syncBuffer

	cancel context.CancelFunc
	done   chan error

	stopOnce sync.Once
	stopErr  error
}

// stop cancels the server's context and returns what Run returned.
func (s *testServer) stop(t *testing.T) error {
	t.Helper()
	s.stopOnce.Do(func() {
		s.cancel()
		select {
		case s.stopErr = <-s.done:
		case <-time.After(15 * time.Second):
			t.Fatal("Run did not return after cancellation")
		}
	})
	return s.stopErr
}

func baseConfig(dbURL string) config.Config {
	return config.Config{
		Env:                   config.EnvProd, // JSON logs, so tests can parse them
		HTTPAddr:              "127.0.0.1:0",
		DatabaseURL:           dbURL,
		DBMaxConns:            4,
		DBMinConns:            1,
		DBStatementTimeout:    5 * time.Second,
		ShutdownTimeout:       5 * time.Second,
		CORSAllowedOrigins:    []string{allowedOrigin},
		LogLevel:              "debug",
		HTTPReadHeaderTimeout: 5 * time.Second,
		HTTPReadTimeout:       5 * time.Second,
		HTTPWriteTimeout:      10 * time.Second,
		HTTPIdleTimeout:       5 * time.Second,
	}
}

// startServer runs the real server against a fresh migrated database.
func startServer(t *testing.T, mutate func(*config.Config), reports ...handler.RouteRegistrar) *testServer {
	t.Helper()

	cfg := baseConfig(testdb.NewMigratedDatabase(t))
	if mutate != nil {
		mutate(&cfg)
	}

	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	s := &testServer{logs: logs, cancel: cancel, done: make(chan error, 1)}
	ready := make(chan net.Addr, 1)

	go func() {
		s.done <- app.Run(ctx, cfg, app.NewLogger(cfg, logs), app.Options{
			Reports: reports,
			Ready:   func(a net.Addr) { ready <- a },
		})
	}()

	select {
	case addr := <-ready:
		s.base = "http://" + addr.String()
	case err := <-s.done:
		cancel()
		t.Fatalf("Run returned before listening: %v", err)
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatal("server did not start listening in time")
	}

	t.Cleanup(func() { _ = s.stop(t) })
	return s
}

// client avoids connection reuse so shutdown and refused-connection checks are not muddied by keep-alives.
func client() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

func get(t *testing.T, target string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	return do(t, http.MethodGet, target, headers)
}

func do(t *testing.T, method, target string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, body
}

func TestServerServesBuiltInEndpoints(t *testing.T) {
	s := startServer(t, nil)

	t.Run("liveness", func(t *testing.T) {
		resp, body := get(t, s.base+"/healthz", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.JSONEq(t, `{"status":"ok"}`, string(body))
	})

	t.Run("readiness reaches the real database and carries a request id", func(t *testing.T) {
		resp, body := get(t, s.base+"/readyz", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.JSONEq(t, `{"status":"ready","checks":{"database":"ok"}}`, string(body))
		_, err := uuid.Parse(resp.Header.Get("X-Request-ID"))
		require.NoError(t, err)
	})

	t.Run("unknown route returns the JSON envelope", func(t *testing.T) {
		resp, body := get(t, s.base+"/api/v1/reports/nothing-here", nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)

		var env dto.ErrorResponse
		require.NoError(t, json.Unmarshal(body, &env))
		require.Equal(t, "not_found", env.Error.Code)
		require.Equal(t, resp.Header.Get("X-Request-ID"), env.Error.RequestID)
	})

	t.Run("CORS preflight for the configured origin", func(t *testing.T) {
		resp, _ := do(t, http.MethodOptions, s.base+"/api/v1/reports/anything", map[string]string{
			"Origin":                        allowedOrigin,
			"Access-Control-Request-Method": "GET",
		})
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
		require.Equal(t, allowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
		require.Equal(t, "GET, OPTIONS", resp.Header.Get("Access-Control-Allow-Methods"))
	})

	t.Run("CORS is refused for other origins", func(t *testing.T) {
		resp, _ := get(t, s.base+"/healthz", map[string]string{"Origin": "https://evil.example"})
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	})
}

func TestServerWritesJSONLogsWithoutLeakingTheDatabasePassword(t *testing.T) {
	s := startServer(t, nil)
	resp, _ := get(t, s.base+"/readyz", nil)
	requestID := resp.Header.Get("X-Request-ID")

	var access map[string]any
	require.Eventually(t, func() bool {
		for _, rec := range s.logs.records(t) {
			if rec["msg"] == "request" && rec["request_id"] == requestID {
				access = rec
				return true
			}
		}
		return false
	}, 3*time.Second, 20*time.Millisecond, "access log line never appeared")

	require.Equal(t, "/readyz", access["route"])
	require.EqualValues(t, 200, access["status"])

	var starting map[string]any
	for _, rec := range s.logs.records(t) {
		if rec["msg"] == "starting" {
			starting = rec
		}
	}
	require.NotNil(t, starting, "startup must be logged")
	require.Contains(t, starting, "addr")

	// The throwaway database URL reuses TEST_POSTGRES_URL's credentials.
	u, err := url.Parse(os.Getenv("TEST_POSTGRES_URL"))
	require.NoError(t, err)
	pw, ok := u.User.Password()
	require.True(t, ok && pw != "", "this check needs a password in TEST_POSTGRES_URL")
	require.NotContains(t, s.logs.String(), pw, "the database password must never reach the logs")
}

func TestServerMountsReportRoutesAndSurvivesPanics(t *testing.T) {
	s := startServer(t, nil, registrarFunc(func(rg *gin.RouterGroup) {
		rg.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
		rg.GET("/panic", func(*gin.Context) { panic("kaboom") })
	}))

	resp, body := get(t, s.base+"/api/v1/reports/ping", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "pong", string(body))

	resp, body = get(t, s.base+"/api/v1/reports/panic", map[string]string{"Origin": allowedOrigin})
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Equal(t, allowedOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
	require.NotContains(t, string(body), "kaboom")

	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "internal_error", env.Error.Code)

	resp, _ = get(t, s.base+"/api/v1/reports/ping", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the server must keep serving after a panic")
}

func TestServerServesTheInvoiceReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/invoices", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.InvoiceListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Outstanding)

	resp, body = get(t, s.base+"/api/v1/reports/invoices/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.InvoiceFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"draft", "sent", "paid", "void"}, filters.Statuses)

	resp, body = get(t, s.base+"/api/v1/reports/invoices?limit=0", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "limit", env.Error.Details[0].Field)
}

func TestServerServesTheEstimateReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/estimates", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.EstimateListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Total)

	resp, body = get(t, s.base+"/api/v1/reports/estimates/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.EstimateFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"draft", "sent", "accepted", "declined"}, filters.Statuses)

	resp, body = get(t, s.base+"/api/v1/reports/estimates?status=paid", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "status", env.Error.Details[0].Field)

	// the invoice report is still served next to it
	resp, body = get(t, s.base+"/api/v1/reports/invoices", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
}

func TestServerServesTheDocumentReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/documents", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.DocumentListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)

	resp, body = get(t, s.base+"/api/v1/reports/documents/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.DocumentFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"draft", "sent", "signed"}, filters.Statuses)
	require.Empty(t, filters.DocumentTypes)

	resp, body = get(t, s.base+"/api/v1/reports/documents?lifecycle=archived", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "lifecycle", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{"/api/v1/reports/invoices", "/api/v1/reports/estimates"} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}

func TestGracefulShutdownLetsInFlightRequestsFinish(t *testing.T) {
	started := make(chan struct{})
	s := startServer(t, nil, registrarFunc(func(rg *gin.RouterGroup) {
		rg.GET("/slow", func(c *gin.Context) {
			close(started)
			time.Sleep(800 * time.Millisecond)
			c.String(http.StatusOK, "finished")
		})
	}))
	addr := strings.TrimPrefix(s.base, "http://")

	type result struct {
		status int
		body   string
		err    error
	}
	results := make(chan result, 1)
	go func() {
		resp, err := client().Get(s.base + "/api/v1/reports/slow")
		if err != nil {
			results <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		results <- result{status: resp.StatusCode, body: string(b), err: err}
	}()

	<-started
	shutdownErr := s.stop(t) // cancels the context while the request is still running

	require.NoError(t, shutdownErr, "a clean shutdown returns nil")
	r := <-results
	require.NoError(t, r.err)
	require.Equal(t, http.StatusOK, r.status)
	require.Equal(t, "finished", r.body, "the in-flight request must complete, not be cut off")

	_, err := net.DialTimeout("tcp", addr, time.Second)
	require.Error(t, err, "the listener must be closed after shutdown")

	var msgs []string
	for _, rec := range s.logs.records(t) {
		msgs = append(msgs, rec["msg"].(string))
	}
	require.Contains(t, msgs, "shutting down")
	require.Contains(t, msgs, "stopped")
}

func TestShutdownTimeoutForcesClose(t *testing.T) {
	started := make(chan struct{})
	s := startServer(t,
		func(cfg *config.Config) { cfg.ShutdownTimeout = 200 * time.Millisecond },
		registrarFunc(func(rg *gin.RouterGroup) {
			rg.GET("/hang", func(c *gin.Context) {
				close(started)
				select {
				case <-c.Request.Context().Done():
				case <-time.After(10 * time.Second):
				}
			})
		}))

	clientErr := make(chan error, 1)
	go func() {
		resp, err := client().Get(s.base + "/api/v1/reports/hang")
		if err == nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()

	<-started
	begin := time.Now()
	err := s.stop(t)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "graceful shutdown")
	require.Less(t, time.Since(begin), 5*time.Second, "must not wait for the hung handler")

	select {
	case cerr := <-clientErr:
		require.Error(t, cerr, "the stuck connection must have been dropped")
	case <-time.After(5 * time.Second):
		t.Fatal("client request was never terminated")
	}
}

func TestRunFailsWhenAddressIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()

	cfg := baseConfig(testdb.NewMigratedDatabase(t))
	cfg.HTTPAddr = taken.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = app.Run(ctx, cfg, app.NewLogger(cfg, &syncBuffer{}), app.Options{})

	require.Error(t, err)
	require.ErrorContains(t, err, "listen on")
}

func TestClientIPHonorsForwardedForOnlyFromTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		proxies []string
		wantIP  string
	}{
		{"no trusted proxies: header is ignored", nil, "127.0.0.1"},
		{"unrelated proxy: header is ignored", []string{"10.1.2.3"}, "127.0.0.1"},
		{"loopback trusted: header is honored", []string{"127.0.0.1"}, "203.0.113.7"},
		{"trusted CIDR: header is honored", []string{"127.0.0.0/8"}, "203.0.113.7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startServer(t, func(cfg *config.Config) { cfg.TrustedProxies = tt.proxies })

			resp, _ := get(t, s.base+"/healthz", map[string]string{"X-Forwarded-For": "203.0.113.7"})
			requestID := resp.Header.Get("X-Request-ID")

			var got any
			require.Eventually(t, func() bool {
				for _, rec := range s.logs.records(t) {
					if rec["msg"] == "request" && rec["request_id"] == requestID {
						got = rec["client_ip"]
						return true
					}
				}
				return false
			}, 3*time.Second, 20*time.Millisecond)
			require.Equal(t, tt.wantIP, got)
		})
	}
}

func TestServerServesThePaymentReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/payments", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.PaymentListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Total)

	resp, body = get(t, s.base+"/api/v1/reports/payments/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.PaymentFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"cash", "check", "card", "other"}, filters.Methods)

	resp, body = get(t, s.base+"/api/v1/reports/payments?method=wire", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "method", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{"/api/v1/reports/invoices", "/api/v1/reports/estimates", "/api/v1/reports/documents"} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}

func TestServerServesTheCreditReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/credits", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.CreditListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Total)

	resp, body = get(t, s.base+"/api/v1/reports/credits/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.CreditFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"cash", "check", "card", "other"}, filters.Methods)

	// credits have no tabs, so the parameter every other report accepts is unsupported here
	resp, body = get(t, s.base+"/api/v1/reports/credits?lifecycle=active", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "lifecycle", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{
		"/api/v1/reports/invoices", "/api/v1/reports/estimates", "/api/v1/reports/documents", "/api/v1/reports/payments",
	} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}

func TestServerServesTheRecurringInvoiceReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/recurring-invoices", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.RecurringListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Total)
	require.Nil(t, list.Range.From, "no default month for this report")
	require.Nil(t, list.Range.To)

	resp, body = get(t, s.base+"/api/v1/reports/recurring-invoices/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.RecurringFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"active", "paused", "failed", "ended"}, filters.Statuses)

	// recurring invoices have no tabs, so the parameter most other reports accept is unsupported here
	resp, body = get(t, s.base+"/api/v1/reports/recurring-invoices?lifecycle=active", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "lifecycle", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{
		"/api/v1/reports/invoices", "/api/v1/reports/estimates", "/api/v1/reports/documents",
		"/api/v1/reports/payments", "/api/v1/reports/credits",
	} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}

func TestServerServesTheNewJobsReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/new-jobs", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.JobListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)
	require.Equal(t, "0.00", list.Summary.Total)

	resp, body = get(t, s.base+"/api/v1/reports/new-jobs/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.JobFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"unconfirmed", "confirmed", "completed", "cancelled"}, filters.Statuses)
	require.NotNil(t, filters.ServiceTypes)
	require.NotNil(t, filters.LeadSources)

	resp, body = get(t, s.base+"/api/v1/reports/new-jobs?lifecycle=active", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "lifecycle", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{
		"/api/v1/reports/invoices", "/api/v1/reports/estimates", "/api/v1/reports/documents",
		"/api/v1/reports/payments", "/api/v1/reports/credits", "/api/v1/reports/recurring-invoices",
	} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}

func TestServerServesTheNewCustomersReportRoutes(t *testing.T) {
	// The database is migrated but not seeded: the report must work, and be empty, rather than fail.
	s := startServer(t, nil)

	resp, body := get(t, s.base+"/api/v1/reports/new-customers", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	var list dto.CustomerListResponse
	require.NoError(t, json.Unmarshal(body, &list))
	require.Empty(t, list.Data)
	require.NotNil(t, list.Summary)
	require.EqualValues(t, 0, list.Summary.Count)

	resp, body = get(t, s.base+"/api/v1/reports/new-customers/filters", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "body: %s", body)
	var filters dto.CustomerFiltersResponse
	require.NoError(t, json.Unmarshal(body, &filters))
	require.Equal(t, []string{"customer", "account_number", "email", "phone", "source", "created_at"}, filters.SortKeys)
	require.NotNil(t, filters.LeadSources)

	resp, body = get(t, s.base+"/api/v1/reports/new-customers?status=active", nil)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	var env dto.ErrorResponse
	require.NoError(t, json.Unmarshal(body, &env))
	require.Equal(t, "status", env.Error.Details[0].Field)

	// the other reports are still served next to it
	for _, path := range []string{
		"/api/v1/reports/invoices", "/api/v1/reports/estimates", "/api/v1/reports/documents",
		"/api/v1/reports/payments", "/api/v1/reports/credits", "/api/v1/reports/recurring-invoices",
		"/api/v1/reports/new-jobs",
	} {
		resp, body = get(t, s.base+path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", path, body)
	}
}
