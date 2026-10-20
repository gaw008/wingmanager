package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gaw008/wingmanager/internal/auth"
	"github.com/gaw008/wingmanager/internal/db"
	"github.com/gaw008/wingmanager/internal/demo"
	"github.com/gaw008/wingmanager/internal/ical"
	"github.com/gaw008/wingmanager/internal/jobs"
	"github.com/gaw008/wingmanager/internal/web"
)

// readHeaderTimeout bounds how long a slow client can occupy a connection before sending headers.
const readHeaderTimeout = 5 * time.Second

// shutdownTimeout allows cancelled requests to record their final sync status.
const shutdownTimeout = 10 * time.Second

// version is replaced with the release tag by the release build.
var version = "dev"

const usage = `Usage: wingmanager <command> [options]

Commands:
  serve           Start the web server
  backup          Copy the database and uploaded files to a new directory
  reset-password  Read a new owner password from stdin and revoke all sessions
  seed-demo       Populate an instance with no owner and print demo credentials
  version         Print the build version

Run wingmanager <command> --help for options and defaults.
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(stdout, usage)
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("a command is required\n%s", usage)
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stdout)
	var dataDir, addr string
	var behindHTTPSProxy, passwordStdin bool
	switch command {
	case "serve", "backup", "reset-password", "seed-demo":
		flags.StringVar(&dataDir, "data-dir", "./data", "Directory containing the SQLite database and uploaded files")
	case "version":
	default:
		return fmt.Errorf("unknown command %q\n%s", command, usage)
	}
	if command == "serve" {
		flags.StringVar(&addr, "addr", "127.0.0.1:8080", "HTTP listen address")
		flags.BoolVar(&behindHTTPSProxy, "behind-https-proxy", false, "Always set Secure on session cookies behind an HTTPS reverse proxy")
	}
	if command == "reset-password" {
		flags.BoolVar(&passwordStdin, "password-stdin", false, "Read the new password from standard input (required)")
	}
	flags.Usage = func() {
		suffix := " [options]"
		if command == "backup" {
			suffix += " <dest>"
		} else if command == "version" {
			suffix = ""
		}
		fmt.Fprintf(stdout, "Usage: wingmanager %s%s\n", command, suffix)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if command == "backup" {
		if flags.NArg() != 1 {
			return errors.New("backup requires one new destination directory")
		}
		if err := db.Backup(context.Background(), dataDir, flags.Arg(0)); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "Backup created: %s\n", flags.Arg(0))
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	if command == "version" {
		_, err := fmt.Fprintln(stdout, version)
		return err
	}
	if command == "reset-password" {
		if !passwordStdin {
			return errors.New("reset-password requires --password-stdin")
		}
		if _, err := os.Stat(filepath.Join(dataDir, "wingmanager.db")); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}
	}
	database, err := db.Open(filepath.Join(dataDir, "wingmanager.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(context.Background(), database); err != nil {
		return err
	}
	switch command {
	case "reset-password":
		password, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		// Accept one terminal line ending without removing password whitespace.
		value := strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r")
		if err := auth.NewService(database).ResetPassword(context.Background(), value); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "Password reset; all sessions revoked.")
		return err
	case "seed-demo":
		credentials, err := demo.Seed(context.Background(), database)
		if credentials.Email != "" {
			_, writeErr := fmt.Fprintf(stdout, "Demo credentials:\nEmail: %s\nPassword: %s\n", credentials.Email, credentials.Password)
			if writeErr != nil {
				return errors.Join(err, writeErr)
			}
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "Demo ready.")
		return err
	}
	service := ical.NewService(database, nil, time.Now)
	handler, err := web.New(database, service, web.Config{DataDir: dataDir, BehindHTTPSProxy: behindHTTPSProxy})
	if err != nil {
		return err
	}
	log.Printf("wingmanager listening on http://%s", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: readHeaderTimeout, BaseContext: func(net.Listener) context.Context { return ctx }}
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
