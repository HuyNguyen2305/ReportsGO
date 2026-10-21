package seed_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"reportsgo/internal/seed"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}

	tests := []struct {
		name    string
		path    string
		want    string
		wantErr error
	}{
		{name: "valid script", path: write("ok.sql", "SELECT 1;"), want: "SELECT 1;"},
		{name: "empty file", path: write("empty.sql", ""), wantErr: seed.ErrEmptyScript},
		{name: "whitespace only", path: write("blank.sql", " \n\t "), wantErr: seed.ErrEmptyScript},
		{name: "missing file", path: filepath.Join(dir, "nope.sql"), wantErr: os.ErrNotExist},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := seed.Load(tt.path)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTablesHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(seed.Tables))
	for _, table := range seed.Tables {
		require.False(t, seen[table], "duplicate table %s", table)
		seen[table] = true
	}
}
