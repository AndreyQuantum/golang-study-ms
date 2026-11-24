// Package app собирает сервис из конкретных реализаций: читает окружение, подключает PostgreSQL и Redis.
// Это единственное место, где знают и про БД, и про кеш.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/cache"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/db"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/memory"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/repository/pg"
	"github.com/AndreyQuantum/golang-study-ms/ledger/internal/service"
)

// New подключается к PostgreSQL и Redis (настройки из окружения), проверяет оба соединения через ctx
// и возвращает сервис и функцию закрытия ресурсов. Миграции не запускаются: схема должна быть применена (goose up).
// При ошибке возвращает nil, nil, err и закрывает всё, что успело открыться.
func New(ctx context.Context) (service.Service, func() error, error) {
	dsn := db.DSNFromEnv()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	log.Printf("connected to PostgreSQL (%s)", db.Redact(dsn))

	cfg, err := cache.ConfigFromEnv()
	if err != nil {
		_ = pool.Close()
		return nil, nil, fmt.Errorf("redis config: %w", err)
	}
	c, err := cache.New(ctx, cfg)
	if err != nil {
		_ = pool.Close()
		return nil, nil, fmt.Errorf("connect to Redis: %w", err)
	}
	log.Printf("connected to Redis (%s, db %d)", cfg.Addr, cfg.DB)

	st := pg.New(pool)
	svc := service.NewCached(service.New(st.Repos(), st), c)

	var once sync.Once
	closeFn := func() error {
		var err error
		once.Do(func() {
			err = errors.Join(c.Close(), pool.Close())
		})
		return err
	}
	return svc, closeFn, nil
}

// NewInMemory собирает сервис на репозиториях в памяти, без кеша: для тестов и демо без БД.
func NewInMemory() service.Service {
	st := memory.New()
	return service.New(st.Repos(), st)
}
