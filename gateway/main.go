package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/gateway/internal/api"
	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

// requestTimeoutFromEnv разбирает REQUEST_TIMEOUT (длительность Go: 50ms, 2s, 1m). Пусто - значение по умолчанию.
// Неверное или неположительное значение - ошибка: опечатка не должна молча давать другой таймаут.
func requestTimeoutFromEnv(raw string) (time.Duration, error) {
	if raw == "" {
		return api.DefaultRequestTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("REQUEST_TIMEOUT=%q: %w (ожидается длительность вроде 500ms или 2s)", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("REQUEST_TIMEOUT=%q: таймаут должен быть больше нуля", raw)
	}
	return d, nil
}

func main() {
	requestTimeout, err := requestTimeoutFromEnv(os.Getenv("REQUEST_TIMEOUT"))
	if err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	initCtx, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	svc, closeFn, err := ledger.New(initCtx)
	if err != nil {
		cancelInit()
		log.Fatalf("init ledger: %v\ncheck that PostgreSQL and Redis are running, DATABASE_URL / DB_* / REDIS_* are correct and migrations are applied (goose up)", err)
	}
	cancelInit()
	defer func() {
		if err := closeFn(); err != nil {
			log.Printf("close ledger: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           api.NewRouter(svc, api.WithTimeout(requestTimeout)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("request timeout: %s", requestTimeout)
		log.Printf("gateway listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
			if cerr := closeFn(); cerr != nil {
				log.Printf("close ledger: %v", cerr)
			}
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Println("shutting down gateway")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		log.Println("gateway stopped")
	}
}
