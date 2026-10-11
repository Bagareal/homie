// Homie: a modular homelab dashboard.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bagareal/homie/internal/config"
	"github.com/Bagareal/homie/internal/server"
	"github.com/Bagareal/homie/internal/status"
	"github.com/Bagareal/homie/internal/webui"
)

// version is set at build time with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	addr := env("HOMIE_ADDR", ":8080")
	dataDir := env("HOMIE_DATA", "./data")

	store, err := config.Open(dataDir)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	checker := status.New(store)
	go checker.Run(ctx)

	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(store, checker, webui.FS(), dataDir, version).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Homie %s listening on %s (data in %s)", version, addr, dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
