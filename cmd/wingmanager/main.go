package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/web"
)

// readHeaderTimeout bounds how long a slow client can occupy a connection before sending headers.
const readHeaderTimeout = 5 * time.Second

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
	handler, err := web.New(database)
	if err != nil {
		return err
	}
	log.Printf("wingmanager listening on http://%s", *addr)
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout}
	return server.ListenAndServe()
}
