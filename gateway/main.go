package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AndreyQuantum/golang-study-ms/gateway/internal/api"
	"github.com/AndreyQuantum/golang-study-ms/ledger"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	initCtx, cancelInit := context.WithTimeout(ctx, 30*time.Second)
	if err := ledger.Init(initCtx); err != nil {
		cancelInit()
		log.Fatalf("init ledger: %v\ncheck that PostgreSQL and Redis are running, DATABASE_URL / DB_* / REDIS_* are correct and migrations are applied (goose up)", err)
	}
	cancelInit()
	defer func() {
		if err := ledger.Close(); err != nil {
			log.Printf("close ledger: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           api.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("gateway listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
			if cerr := ledger.Close(); cerr != nil {
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
