package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr     string
	Password string
	DB       int
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{Addr: "localhost:6379", Password: os.Getenv("REDIS_PASSWORD")}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("REDIS_DB must be a non-negative integer, got %q", v)
		}
		cfg.DB = n
	}
	return cfg, nil
}

type Cache struct {
	client *redis.Client
}

func New(ctx context.Context, cfg Config) (*Cache, error) {
	client := redis.NewClient(&redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis %s: %w", cfg.Addr, err)
	}
	return &Cache{client: client}, nil
}

func (c *Cache) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	data, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis get %s: %w", key, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, fmt.Errorf("decode cached %s: %w", key, err)
	}
	return true, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key, err)
	}
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

func (c *Cache) Close() error {
	return c.client.Close()
}
