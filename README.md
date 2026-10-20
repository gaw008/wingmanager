# wingmanager

[简体中文](README.zh-CN.md)

An open-source PMS for individual landlords managing 1–20 units, with both
short-term and long-term rentals. One Go binary, SQLite, and an embedded
Chinese/English web UI; no external database or frontend build is needed.

Today answers: who checks in or out, which tenants owe rent, and how much you
have earned this year. v0.1 includes manual bookings, iCal import/export,
leases and rent charges, payments and deposits, cash-basis reports and Schedule E
CSV, tasks, and attachments. iCal is calendar sync, not two-way channel management.

## Download or build

Download your platform's binary from [Releases](https://github.com/gaw008/wingmanager/releases)
and rename it to `wingmanager` (`wingmanager.exe` on Windows). Releases target
Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. On macOS/Linux, make the
download executable:

```sh
chmod +x ./wingmanager
```

Alternatively, from a source checkout with Go 1.27.3 or newer:

```sh
go build -o wingmanager ./cmd/wingmanager
./wingmanager version
./wingmanager --help
```

Local builds print `dev`; release builds print their injected `v*` tag. The
following examples use a macOS/Linux shell. Windows users can run the same binary
subcommands in their terminal, using the `.exe` filename.

## Try the demo in a few minutes

From the directory containing the binary, with no existing `./data` instance:

```sh
./wingmanager seed-demo --data-dir ./data
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data
```

Keep the server running. Open <http://127.0.0.1:8080> and log in with the email and
random password printed by `seed-demo`. Today shows a check-in, a check-out, an
overdue tenant, cleaning work, and YTD net income of **625.00 USD**. The two
properties and all people, bookings, and money records are synthetic. Dates are
relative to the instance's current date. The example iCal feed uses a reserved
`.invalid` address, so its sync error is expected; it never contacts a real host's
calendar. Change language, timezone, or currency in Settings.

Seeding refuses any instance that already has an account. It commits through
existing domain operations; a failure can leave partial demo data. If that happens,
the command reports the error and prints any created credentials. Use a fresh
data directory for another demo attempt.

For your own empty instance, skip seeding and start `serve` instead. The first
visit opens owner setup. Passwords require at least 8 characters and at most 72
UTF-8 bytes. Stop the foreground server with Ctrl-C.

## Data and command options

```sh
./wingmanager serve --help
```

Defaults are `--addr 127.0.0.1:8080`, `--data-dir ./data`, and
`--behind-https-proxy=false`. Each data command accepts `--data-dir`; put options
before the backup destination. The data directory contains `wingmanager.db` and
uploaded `files/`. Migrations run automatically on startup. UI assets and timezone
data are embedded; internet access is needed for external iCal feeds.

## Backup and restore

While the server is running, in another terminal:

```sh
./wingmanager backup --data-dir ./data ./backup-v01
```

The destination must be a new directory outside `data/files`; an existing file or
directory is refused. It receives a `VACUUM INTO` database snapshot plus `files/`.
Writes wait while the snapshot and files are copied. Copy the whole backup
directory to your backup storage.

To restore, stop the server first. Preserve the old directory, then copy the
backup into its place (both destination names below must be unused):

```sh
mv ./data ./data-before-restore
cp -R ./backup-v01 ./data
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data
```

Restore the entire directory, including attachments, with ownership writable by
the server user. No restore command is needed.

## Reset a forgotten password

From a terminal on the server, pass the new password through standard input.
Replace the example password before using this with real data:

```sh
printf '%s\n' 'a-new-example-password' | ./wingmanager reset-password --data-dir ./data --password-stdin
```

This uses the setup password rule and revokes all sessions. Log in again with
the existing email and the new password. One trailing line ending is removed;
password spaces are preserved.

## Docker

From a source checkout, with Docker Engine running:

```sh
docker build -t wingmanager:local .
docker run --rm -p 127.0.0.1:8080:8080 -v wingmanager-data:/data wingmanager:local
```

Open the same local URL and complete owner setup. The named volume persists when
the container stops. The multi-stage image compiles with CGO disabled, runs as a
non-root user on distroless static, and defaults to
HTTP on `0.0.0.0:8080` with data under `/data`. For bind mounts, the directory must
be writable by UID/GID 65532. Tagged releases also publish an image to
`ghcr.io/gaw008/wingmanager` with the same version tag.

## HTTPS with Caddy

Point your domain's DNS to the server and allow ports 80/443 to reach Caddy.
Start wingmanager locally with secure session cookies:

```sh
./wingmanager serve --addr 127.0.0.1:8080 --data-dir ./data --behind-https-proxy
```

Use this Caddyfile, replacing the domain, and load it using your Caddy service:

```caddyfile
rentals.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy manages HTTPS certificates ([official setup guide](https://caddyserver.com/docs/quick-starts/reverse-proxy)).
Access wingmanager through the HTTPS domain. The proxy flag always adds `Secure`
to session cookies, even though Caddy talks HTTP to wingmanager. Forwarded request
headers do not control cookie security. Keep the backend listening on loopback.

## Development

Install the pinned sqlc tool; regenerate after changing domain queries:

```sh
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
sqlc generate
```

Required checks:

```sh
gofmt -l .
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
sqlc diff
go test -race ./...
```

Tests use temporary real SQLite databases; only network boundaries are faked.
The separate release workflow builds five platform binaries and a Linux
amd64/arm64 GHCR image on a `v*` tag. The existing CI workflow remains unchanged.
See [PLAN](docs/PLAN.md) and [M8 decisions](docs/decisions/0009-m8.md).

## License

AGPL-3.0; see [LICENSE](LICENSE). Vendored assets retain their upstream licenses
in `internal/web/static/`.
