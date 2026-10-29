package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"reportsgo/internal/config"
	"reportsgo/internal/handler"
	"reportsgo/internal/middleware"
	"reportsgo/internal/repository"
	"reportsgo/internal/service"
)

// Options carries the parts of the server that vary between the real binary and tests.
type Options struct {
	// Reports are mounted under /api/v1/reports.
	Reports []handler.RouteRegistrar
	// Ready, if set, is called with the bound address once the server is listening.
	Ready func(addr net.Addr)
}

// Run starts the API and blocks until ctx is cancelled or the server fails. On cancellation it stops
// accepting connections, lets in-flight requests finish (up to cfg.ShutdownTimeout), and only then closes
// the database pool. A clean shutdown returns nil.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, opts Options) error {
	pool, err := repository.NewPool(ctx, repository.PoolConfig{
		URL:              cfg.DatabaseURL,
		MaxConns:         cfg.DBMaxConns,
		MinConns:         cfg.DBMinConns,
		StatementTimeout: cfg.DBStatementTimeout,
	})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close() // runs after the server has drained, so requests never lose their database

	// The reports that default to the current month read it from this clock, in the business time zone.
	clock := ClockIn(cfg.Location)

	// Each report is wired the same way: repository -> service -> handler (a RouteRegistrar).
	invoices := handler.NewInvoiceHandler(
		service.NewInvoiceService(repository.NewInvoiceRepository(pool), clock), logger)
	estimates := handler.NewEstimateHandler(
		service.NewEstimateService(repository.NewEstimateRepository(pool), clock), logger)
	documents := handler.NewDocumentHandler(
		service.NewDocumentService(repository.NewDocumentRepository(pool), clock), logger)
	payments := handler.NewPaymentHandler(
		service.NewPaymentService(repository.NewPaymentRepository(pool), clock), logger)
	credits := handler.NewCreditHandler(
		service.NewCreditService(repository.NewCreditRepository(pool), clock), logger)
	recurring := handler.NewRecurringHandler(
		service.NewRecurringService(repository.NewRecurringRepository(pool)), logger)
	jobs := handler.NewJobHandler(
		service.NewJobService(repository.NewJobRepository(pool), clock), logger)
	customers := handler.NewCustomerHandler(
		service.NewCustomerService(repository.NewCustomerRepository(pool), clock), logger)
	totalSales := handler.NewTotalSalesHandler(
		service.NewTotalSalesService(repository.NewTotalSalesRepository(pool), clock), logger)
	paymentsCollected := handler.NewPaymentsCollectedHandler(
		service.NewPaymentsCollectedService(repository.NewPaymentsCollectedRepository(pool), clock), logger)

	engine := handler.NewRouter(handler.Deps{
		Logger:     logger,
		Pinger:     pool,
		Middleware: middleware.Default(logger, cfg.CORSAllowedOrigins),
		Reports:    append([]handler.RouteRegistrar{invoices, estimates, documents, payments, credits, recurring, jobs, customers, totalSales, paymentsCollected}, opts.Reports...),
	})
	// An empty list means "trust no proxy header", so clients cannot forge client_ip via X-Forwarded-For.
	if err := engine.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return fmt.Errorf("set trusted proxies: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	// No BaseContext derived from ctx: cancelling ctx must not cancel in-flight requests,
	// otherwise graceful shutdown would abort them instead of letting them finish.
	server := &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		ReadTimeout:       cfg.HTTPReadTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.Info("starting", "addr", ln.Addr().String(), "config", cfg)
	if opts.Ready != nil {
		opts.Ready(ln.Addr())
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ln) }()

	select {
	case err := <-serveErr:
		// Serve only returns on failure here; Shutdown has not been called yet.
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		// Out of time: drop the remaining connections so the process can exit.
		_ = server.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	logger.Info("stopped")
	return nil
}

// ClockIn returns a clock that reads the current time in loc; a nil loc means UTC.
func ClockIn(loc *time.Location) service.Clock {
	if loc == nil {
		loc = time.UTC
	}
	return func() time.Time { return time.Now().In(loc) }
}
