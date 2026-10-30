package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func DSNFromEnv() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(envOr("DB_USER", "postgres"), envOr("DB_PASS", "postgres")),
		Host:     net.JoinHostPort(envOr("DB_HOST", "localhost"), envOr("DB_PORT", "5432")),
		Path:     "/" + envOr("DB_NAME", "cashapp"),
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

var kvPassword = regexp.MustCompile(`(?i)(password\s*=\s*)('(?:[^'\\]|\\.)*'|\S+)`)

func Redact(dsn string) string {
	if !strings.Contains(dsn, "://") {
		return kvPassword.ReplaceAllString(dsn, "${1}xxxxx")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "<unparseable dsn>"
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	q := u.Query()
	for k := range q {
		if strings.EqualFold(k, "password") {
			q.Set(k, "xxxxx")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", Redact(dsn), err)
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.PingContext(pingCtx); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("ping database %s: %w", Redact(dsn), err)
	}
	return pool, nil
}
