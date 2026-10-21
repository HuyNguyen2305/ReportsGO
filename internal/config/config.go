// Package config loads and validates the application's environment-based configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Environment names accepted by APP_ENV.
const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config holds every setting the API reads from the environment.
type Config struct {
	Env                string
	HTTPAddr           string
	DatabaseURL        string
	DBMaxConns         int32
	DBMinConns         int32
	DBStatementTimeout time.Duration
	ShutdownTimeout    time.Duration
	CORSAllowedOrigins []string
	LogLevel           string

	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	// TrustedProxies lists the proxies (IPs or CIDRs) whose X-Forwarded-For header is believed.
	// Empty means no proxy is trusted and the client IP is the connection's remote address.
	TrustedProxies []string
}

// Lookup reports the value of an environment variable, like os.LookupEnv.
type Lookup func(key string) (string, bool)

// Load reads and validates the configuration. All problems are reported together,
// each naming the offending variable.
func Load(lookup Lookup) (Config, error) {
	l := loader{lookup: lookup}

	cfg := Config{
		Env:                l.oneOf("APP_ENV", EnvDev, EnvDev, EnvProd),
		HTTPAddr:           l.str("HTTP_ADDR", ":8080"),
		DatabaseURL:        l.required("DATABASE_URL"),
		DBMaxConns:         l.positiveInt32("DB_MAX_CONNS", 10),
		DBMinConns:         l.nonNegativeInt32("DB_MIN_CONNS", 2),
		DBStatementTimeout: l.duration("DB_STATEMENT_TIMEOUT", 5*time.Second),
		ShutdownTimeout:    l.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		CORSAllowedOrigins: l.list("CORS_ALLOWED_ORIGINS"),
		LogLevel:           l.oneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error"),

		HTTPReadHeaderTimeout: l.duration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		HTTPReadTimeout:       l.duration("HTTP_READ_TIMEOUT", 15*time.Second),
		HTTPWriteTimeout:      l.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
		HTTPIdleTimeout:       l.duration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		TrustedProxies:        l.list("TRUSTED_PROXIES"),
	}

	if msg := checkListenAddr(cfg.HTTPAddr); msg != "" {
		l.fail("HTTP_ADDR", fmt.Sprintf("%q %s", cfg.HTTPAddr, msg))
	}
	for _, p := range cfg.TrustedProxies {
		if !validProxy(p) {
			l.fail("TRUSTED_PROXIES", fmt.Sprintf("%q must be an IP address or CIDR such as 10.0.0.0/8", p))
		}
	}
	for _, origin := range cfg.CORSAllowedOrigins {
		if msg := checkOrigin(origin); msg != "" {
			l.fail("CORS_ALLOWED_ORIGINS", fmt.Sprintf("%q %s", origin, msg))
		}
	}
	if cfg.DBMinConns > cfg.DBMaxConns {
		l.errs = append(l.errs, errors.New("DB_MIN_CONNS: must not exceed DB_MAX_CONNS"))
	}
	if cfg.DatabaseURL != "" {
		if _, err := url.Parse(cfg.DatabaseURL); err != nil {
			// url.Error includes the raw URL, which holds the password; report only the cause.
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			l.errs = append(l.errs, fmt.Errorf("DATABASE_URL: not a valid URL: %w", err))
		}
	}

	if err := errors.Join(l.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// SlogLevel converts LogLevel to a slog.Level.
func (c Config) SlogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogValue implements slog.LogValuer so the database password never reaches the logs.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.Int("db_max_conns", int(c.DBMaxConns)),
		slog.Int("db_min_conns", int(c.DBMinConns)),
		slog.Duration("db_statement_timeout", c.DBStatementTimeout),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Any("cors_allowed_origins", c.CORSAllowedOrigins),
		slog.String("log_level", c.LogLevel),
		slog.Duration("http_read_header_timeout", c.HTTPReadHeaderTimeout),
		slog.Duration("http_read_timeout", c.HTTPReadTimeout),
		slog.Duration("http_write_timeout", c.HTTPWriteTimeout),
		slog.Duration("http_idle_timeout", c.HTTPIdleTimeout),
		slog.Any("trusted_proxies", c.TrustedProxies),
	)
}

// checkListenAddr returns a problem description, or "" if addr is a host:port with a numeric port.
func checkListenAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "must be host:port, such as :8080"
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "must have a numeric port between 0 and 65535"
	}
	return ""
}

// validProxy reports whether s is an IP address or a CIDR prefix.
func validProxy(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	_, err := netip.ParsePrefix(s)
	return err == nil
}

// checkOrigin returns a problem description, or "" if origin is a plain http(s) origin
// (scheme, host and optional port, nothing else).
func checkOrigin(origin string) string {
	if origin == "*" {
		return "is not allowed; list explicit origins such as https://app.example.com"
	}
	u, err := url.Parse(origin)
	switch {
	case err != nil:
		return "is not a valid URL"
	case u.Scheme != "http" && u.Scheme != "https":
		return "must start with http:// or https://"
	case u.Host == "" || u.Hostname() == "":
		return "must include a host"
	case u.User != nil:
		return "must not contain credentials"
	case u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/"):
		return "must be an origin only, without a path, query or fragment"
	}
	return ""
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid>"
	}
	return u.Redacted()
}

// loader reads variables and accumulates validation errors.
type loader struct {
	lookup Lookup
	errs   []error
}

func (l *loader) get(key string) (string, bool) {
	v, ok := l.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (l *loader) fail(key, msg string) {
	l.errs = append(l.errs, fmt.Errorf("%s: %s", key, msg))
}

func (l *loader) str(key, def string) string {
	if v, ok := l.get(key); ok {
		return v
	}
	return def
}

func (l *loader) required(key string) string {
	v, ok := l.get(key)
	if !ok {
		l.fail(key, "is required")
	}
	return v
}

func (l *loader) oneOf(key, def string, allowed ...string) string {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	l.fail(key, fmt.Sprintf("must be one of %s", strings.Join(allowed, ", ")))
	return def
}

func (l *loader) positiveInt32(key string, def int32) int32 {
	return l.int32(key, def, 1, "a positive integer")
}

func (l *loader) nonNegativeInt32(key string, def int32) int32 {
	return l.int32(key, def, 0, "a non-negative integer")
}

func (l *loader) int32(key string, def, minimum int32, what string) int32 {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < int64(minimum) {
		l.fail(key, "must be "+what)
		return def
	}
	return int32(n)
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		l.fail(key, "must be a positive duration such as 5s or 1m")
		return def
	}
	return d
}

func (l *loader) list(key string) []string {
	v, ok := l.get(key)
	if !ok {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
