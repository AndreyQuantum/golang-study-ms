package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type payload struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := New(context.Background(), Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, mr
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", "")
		t.Setenv("REDIS_DB", "")
		t.Setenv("REDIS_PASSWORD", "")
		cfg, err := ConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, Config{Addr: "localhost:6379"}, cfg)
	})
	t.Run("custom", func(t *testing.T) {
		t.Setenv("REDIS_ADDR", "redis:6380")
		t.Setenv("REDIS_DB", "3")
		t.Setenv("REDIS_PASSWORD", "pw")
		cfg, err := ConfigFromEnv()
		require.NoError(t, err)
		assert.Equal(t, Config{Addr: "redis:6380", Password: "pw", DB: 3}, cfg)
	})
	t.Run("bad db", func(t *testing.T) {
		t.Setenv("REDIS_DB", "abc")
		_, err := ConfigFromEnv()
		assert.Error(t, err)
	})
	t.Run("negative db", func(t *testing.T) {
		t.Setenv("REDIS_DB", "-1")
		_, err := ConfigFromEnv()
		assert.Error(t, err)
	})
}

func TestNewUnreachable(t *testing.T) {
	_, err := New(context.Background(), Config{Addr: "127.0.0.1:1"})
	assert.Error(t, err)
}

func TestSetGetJSON(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.SetJSON(ctx, "k", payload{Name: "a", Value: 1.5}, time.Minute))

	var got payload
	ok, err := c.GetJSON(ctx, "k", &got)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, payload{Name: "a", Value: 1.5}, got)
	assert.Equal(t, time.Minute, mr.TTL("k"))
}

func TestGetJSONMiss(t *testing.T) {
	c, _ := newTestCache(t)
	var got payload
	ok, err := c.GetJSON(context.Background(), "missing", &got)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestGetJSONCorrupted(t *testing.T) {
	c, mr := newTestCache(t)
	require.NoError(t, mr.Set("k", "not json"))
	var got payload
	ok, err := c.GetJSON(context.Background(), "k", &got)
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestExpiry(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()
	require.NoError(t, c.SetJSON(ctx, "k", payload{Name: "a"}, 30*time.Second))
	mr.FastForward(31 * time.Second)
	var got payload
	ok, err := c.GetJSON(ctx, "k", &got)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDelete(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()
	require.NoError(t, c.SetJSON(ctx, "a", 1, time.Minute))
	require.NoError(t, c.SetJSON(ctx, "b", 2, time.Minute))
	require.NoError(t, c.Delete(ctx, "a", "b"))
	require.NoError(t, c.Delete(ctx))
	var n int
	ok, err := c.GetJSON(ctx, "a", &n)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestErrorsWhenServerDown(t *testing.T) {
	c, mr := newTestCache(t)
	mr.Close()
	ctx := context.Background()
	var n int
	_, err := c.GetJSON(ctx, "k", &n)
	assert.Error(t, err)
	assert.Error(t, c.SetJSON(ctx, "k", 1, time.Minute))
	assert.Error(t, c.Delete(ctx, "k"))
}
