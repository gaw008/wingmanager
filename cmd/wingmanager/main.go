package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/jobs"
	"github.com/gaw008/wingmanager/internal/web"
)

// readHeaderTimeout bounds how long a slow client can occupy a connection before sending headers.
const readHeaderTimeout = 5 * time.Second

// shutdownTimeout allows cancelled requests to record their final sync status.
const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: wingmanager serve [--addr 127.0.0.1:8080] [--data-dir ./data]")
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := flags.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dataDir := flags.String("data-dir", "./data", "Directory containing the SQLite database")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		return err
	}
	database, err := db.Open(filepath.Join(*dataDir, "wingmanager.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(context.Background(), database); err != nil {
		return err
	}
	service := ical.NewService(database, nil, time.Now)
	handler, err := web.New(database, service)
	if err != nil {
		return err
	}
	log.Printf("wingmanager listening on http://%s", *addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout, BaseContext: func(net.Listener) context.Context { return ctx }}
	jobsDone := make(chan struct{})
	go func() { defer close(jobsDone); jobs.Run(ctx, database, service) }()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ListenAndServe() }()
	select {
	case err := <-serverDone:
		stop()
		<-jobsDone
		return err
	case <-ctx.Done():
		// Request and job contexts are already cancelled; drain handlers before DB close.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		<-jobsDone
		serveErr := <-serverDone
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(err, serveErr)
	}
}
