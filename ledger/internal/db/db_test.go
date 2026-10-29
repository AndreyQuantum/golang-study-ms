package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASS", "DB_NAME"} {
		t.Setenv(k, "")
	}
}

func TestDSNFromEnvDefaults(t *testing.T) {
	clearEnv(t)
	assert.Equal(t, "postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable", DSNFromEnv())
}

func TestDSNFromEnvParts(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "db.local")
	t.Setenv("DB_PORT", "6543")
	t.Setenv("DB_USER", "app")
	t.Setenv("DB_PASS", "p@ss word")
	t.Setenv("DB_NAME", "money")
	assert.Equal(t, "postgres://app:p%40ss%20word@db.local:6543/money?sslmode=disable", DSNFromEnv())
}

func TestDSNFromEnvDatabaseURLWins(t *testing.T) {
	clearEnv(t)
	t.Setenv("DB_HOST", "ignored")
	t.Setenv("DATABASE_URL", "postgres://u:p@h:1/d")
	assert.Equal(t, "postgres://u:p@h:1/d", DSNFromEnv())
}

func TestRedact(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"with password", "postgres://u:secret@h:5432/d?sslmode=disable", "postgres://u:xxxxx@h:5432/d?sslmode=disable"},
		{"without password", "postgres://u@h:5432/d", "postgres://u@h:5432/d"},
		{"without user", "postgres://h:5432/d", "postgres://h:5432/d"},
		{"keyword value", "host=h user=u password=secret dbname=d", "host=h user=u password=xxxxx dbname=d"},
		{"keyword quoted", "host=h password='se cret' dbname=d", "host=h password=xxxxx dbname=d"},
		{"unparsable", "postgres://u:p%zz@h/d", "<unparseable dsn>"},
		{"query password", "postgres://h/d?password=secret&sslmode=disable", "postgres://h/d?password=xxxxx&sslmode=disable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.dsn)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "secret")
		})
	}
}

func TestOpenUnreachable(t *testing.T) {
	_, err := Open(context.Background(), "postgres://u:secret@127.0.0.1:1/d?sslmode=disable&connect_timeout=1")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.Contains(t, err.Error(), "xxxxx")
}
