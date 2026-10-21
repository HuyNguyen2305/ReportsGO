package config_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/config"
)

func loadWith(t *testing.T, extra map[string]string) (config.Config, error) {
	t.Helper()
	env := map[string]string{"DATABASE_URL": validURL}
	for k, v := range extra {
		env[k] = v
	}
	return config.Load(lookupFrom(env))
}

func TestLoadParsesHTTPServerSettings(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{
		"HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT":        "10s",
		"HTTP_WRITE_TIMEOUT":       "1m",
		"HTTP_IDLE_TIMEOUT":        "90s",
		"TRUSTED_PROXIES":          "10.0.0.0/8, 192.168.1.5 ,::1, 2001:db8::/32",
	})
	require.NoError(t, err)

	require.Equal(t, 2*time.Second, cfg.HTTPReadHeaderTimeout)
	require.Equal(t, 10*time.Second, cfg.HTTPReadTimeout)
	require.Equal(t, time.Minute, cfg.HTTPWriteTimeout)
	require.Equal(t, 90*time.Second, cfg.HTTPIdleTimeout)
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.5", "::1", "2001:db8::/32"}, cfg.TrustedProxies)
}

func TestLoadRejectsInvalidHTTPDurations(t *testing.T) {
	for _, name := range []string{
		"HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
	} {
		for label, value := range map[string]string{"not a duration": "soon", "zero": "0s", "negative": "-5s", "no unit": "30"} {
			t.Run(name+"/"+label, func(t *testing.T) {
				_, err := loadWith(t, map[string]string{name: value})
				require.ErrorContains(t, err, name+": must be a positive duration")
			})
		}
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"single IPv4", "10.1.2.3", false},
		{"IPv4 CIDR", "172.16.0.0/12", false},
		{"IPv6", "fd00::1", false},
		{"IPv6 CIDR", "fd00::/8", false},
		{"several", "10.0.0.1,10.0.0.2", false},
		{"hostname", "proxy.internal", true},
		{"bad prefix length", "10.0.0.0/33", true},
		{"garbage", "not-an-ip", true},
		{"wildcard", "*", true},
		{"one bad among good", "10.0.0.1,nope", true},
		{"URL", "http://10.0.0.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWith(t, map[string]string{"TRUSTED_PROXIES": tt.value})
			if tt.wantErr {
				require.ErrorContains(t, err, "TRUSTED_PROXIES")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadHTTPAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"port only", ":8080", false},
		{"ephemeral port", ":0", false},
		{"IPv4 host", "127.0.0.1:0", false},
		{"IPv6 host", "[::1]:9000", false},
		{"hostname", "localhost:8080", false},
		{"max port", ":65535", false},
		{"no port", "localhost", true},
		{"bare number", "8080", true},
		{"non-numeric port", ":http", true},
		{"empty port", "localhost:", true},
		{"port too large", ":65536", true},
		{"negative port", ":-1", true},
		{"too many colons", "::1:8080", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWith(t, map[string]string{"HTTP_ADDR": tt.addr})
			if tt.wantErr {
				require.ErrorContains(t, err, "HTTP_ADDR")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadReportsHTTPProblemsTogether(t *testing.T) {
	_, err := loadWith(t, map[string]string{
		"HTTP_ADDR":          "nope",
		"HTTP_WRITE_TIMEOUT": "0s",
		"TRUSTED_PROXIES":    "proxy.internal",
	})
	require.ErrorContains(t, err, "HTTP_ADDR")
	require.ErrorContains(t, err, "HTTP_WRITE_TIMEOUT")
	require.ErrorContains(t, err, "TRUSTED_PROXIES")
}

func TestLogValueIncludesHTTPSettings(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8", "HTTP_IDLE_TIMEOUT": "45s"})
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("config", "config", cfg)

	out := buf.String()
	require.Contains(t, out, "config.http_idle_timeout=45s")
	require.Contains(t, out, "config.http_read_header_timeout=5s")
	require.Contains(t, out, "10.0.0.0/8")
	require.NotContains(t, out, "s3cret")
}
