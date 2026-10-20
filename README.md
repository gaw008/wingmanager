# wingmanager

An open-source property management system for individual landlords managing
short-term and long-term rentals. Go, SQLite, htmx, and Pico.css in a single binary.

Milestone M1 provides owner setup, login, Chinese/English UI, and instance settings.
Rental management arrives in later milestones; see [the plan](docs/PLAN.md).

## Build and run

With Go 1.27.3 or newer:

```sh
go build ./cmd/wingmanager
./wingmanager serve
```

Open <http://127.0.0.1:8080>, create the owner account, then log in. Change the
landlord name, timezone, currency, or language under Settings.

```sh
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data
./wingmanager serve --help
```

The data directory is created automatically. SQLite lives in
`<data-dir>/wingmanager.db`. Migrations run automatically on startup. Copy the
built binary to the target machine; runtime needs no network connection or Go
installation. Templates, timezone data, htmx 2.0.4, and Pico.css 2.0.6 are embedded.

## Development

Install sqlc v1.31.1, then run `sqlc generate` after editing domain queries.
Generated Go files belong in version control. Checks:

```sh
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
sqlc diff
go test -race ./...
```

## License

AGPL-3.0; see [LICENSE](LICENSE). Vendored assets retain their upstream licenses
in `internal/web/static/`.
