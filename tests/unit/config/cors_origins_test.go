package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/config"
)

func TestLoadAcceptsPlainOrigins(t *testing.T) {
	cfg, err := config.Load(lookupFrom(map[string]string{
		"DATABASE_URL": validURL,
		"CORS_ALLOWED_ORIGINS": "https://app.example.com, http://localhost:3000, " +
			"https://reports.example.com:8443, https://trailing.example.com/",
	}))
	require.NoError(t, err)
	require.Equal(t, []string{
		"https://app.example.com",
		"http://localhost:3000",
		"https://reports.example.com:8443",
		"https://trailing.example.com/",
	}, cfg.CORSAllowedOrigins)
}

func TestLoadRejectsInvalidOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		wantErr string
	}{
		{"wildcard", "*", `"*" is not allowed`},
		{"wildcard among valid origins", "https://ok.example.com,*", `"*" is not allowed`},
		{"no scheme", "app.example.com", "must start with http:// or https://"},
		{"scheme-relative", "//app.example.com", "must start with http:// or https://"},
		{"ftp scheme", "ftp://app.example.com", "must start with http:// or https://"},
		{"scheme only", "https://", "must include a host"},
		{"credentials", "https://user:pw@app.example.com", "must not contain credentials"},
		{"path", "https://app.example.com/reports", "must be an origin only"},
		{"query", "https://app.example.com?x=1", "must be an origin only"},
		{"fragment", "https://app.example.com#top", "must be an origin only"},
		{"unparseable", "https://app .example.com", "is not a valid URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(lookupFrom(map[string]string{
				"DATABASE_URL":         validURL,
				"CORS_ALLOWED_ORIGINS": tt.origin,
			}))
			require.ErrorContains(t, err, "CORS_ALLOWED_ORIGINS")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLoadReportsEveryInvalidOrigin(t *testing.T) {
	_, err := config.Load(lookupFrom(map[string]string{
		"DATABASE_URL":         validURL,
		"CORS_ALLOWED_ORIGINS": "*,ftp://x.example.com",
	}))
	require.ErrorContains(t, err, `"*" is not allowed`)
	require.ErrorContains(t, err, `"ftp://x.example.com" must start with`)
}
