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

	"github.com/Bagareal/homie/internal/auth"
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
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		resetPassword()
		return
	}
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

	app := server.New(store, checker, webui.FS(), dataDir, version).WithReleasesURL(os.Getenv("HOMIE_RELEASES_URL"))
	if err := app.AuthError(); err != nil {
		// Closed, not open: every page of the API answers with the reason.
		log.Printf("login: %v", err)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Handler(),
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

// resetPassword is "homie reset-password": it makes the running server forget the
// password (the user name stays), so the next visit to the page asks for a new one,
// and prints the code that needs (so nobody else on the network can do it first).
func resetPassword() {
	dataDir := env("HOMIE_DATA", "./data")
	code, err := auth.Reset(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset-password: %v\n", err)
		os.Exit(1)
	}
	fmt.Print("The password is reset the next time the page is opened (every device is signed out; the dashboard and the widgets are not touched).\n")
	fmt.Printf("Open Homie in the browser and choose the new password with this code:\n\n    %s\n\n", code)
	fmt.Printf("Data folder used: %s (set HOMIE_DATA if it is another one).\n", dataDir)
}
