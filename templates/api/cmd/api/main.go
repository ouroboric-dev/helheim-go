// Command api serves the HTTP API on HOST:PORT (all interfaces and port 8080 when unset), with the
// database at DATABASE_URL when that is set.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"api/internal/httpapi"
	"api/internal/store"
)

func main() {
	port, err := portFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var deps httpapi.Deps
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pool, err := store.Open(ctx, url)
		if err != nil {
			log.Fatal(err)
		}
		defer pool.Close()
		deps.DB = pool
	}
	srv := &http.Server{Addr: listenAddr(port), Handler: httpapi.New(deps), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// portFromEnv reads PORT: a whole number from 1 to 65535, or 8080 when it is unset.
func portFromEnv() (int, error) {
	v := os.Getenv("PORT")
	if v == "" {
		return 8080, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("PORT must be a whole number from 1 to 65535")
	}
	return port, nil
}

// listenAddr is HOST:PORT; with no HOST it is :PORT, all interfaces.
func listenAddr(port int) string {
	return net.JoinHostPort(os.Getenv("HOST"), strconv.Itoa(port))
}
