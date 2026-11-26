# ROMHarbor

A self-hosted web application for browsing ROM catalogs, finding games, and downloading them
into a library organized by platform. Run it on your own server and keep your library on your own storage.

## What it does

- Browse platforms and search their game listings.
- View game details, available files, and checksums when provided by the source.
- Start downloads and follow their progress in the web interface. Downloads are queued in the
  application database and run by the download worker, one at a time, retry failed attempts,
  and resume after a restart.
- Save games into platform folders in your library, ready for your own library manager or emulator.
- Open archives so the library holds the plain ROM or disc image each emulator loads (`.z64`,
  `.cue` + `.bin`, `.chd`), not a container.
- Keep multi-disc games complete: starting one disc ("Disc 1", "Disk 2", "CD 3", "Disc 1 of 2")
  also queues the other discs of the same release, and the platform folder gets a
  `<Game title>.m3u` playlist (for example `Driver 2 (USA).m3u`) that lists the discs in order with
  paths relative to the playlist. Load the `.m3u` and RetroArch cores, Knulli and standalone
  emulators swap discs from their own menu. Installed discs are never moved, and a game with one
  disc gets no playlist.
- Cache listings locally to avoid fetching them on every visit.

Archive.org is the default source. `dlarchive` and `lolroms` are opt-in; `lolroms` requires search text.
Source availability and metadata depend on the upstream provider.

For a finished PlayStation download, `GET /api/job/readiness?id=<job-id>` checks the installed
files and reports its entry point, bundle contents, missing files or declared discs, and any
uncertainty about the set. A complete bundle is not a guarantee of emulator compatibility.
The `compatibility` array gives separate results for Knulli, PCSX ReARMed, and Beetle PSX HW:
`format_supported`, `format_unsupported`, `requirements_missing`, or `unknown`. Each result
links to its format evidence. Format support describes the entry point only; the report does
not inspect destination firmware or settings, and `gameplay_verified` is always `false`.

`GET /api/job/bundle?id=<job-id>` downloads a PlayStation M3U playlist and its referenced
files as one ZIP. Extract the ZIP into a destination folder and open the M3U; keep the
extracted paths together. A missing file, known missing disc, or unsafe reference makes
the bundle unavailable (`409`). If the source did not declare a disc count, the ZIP can
still contain every playlist reference without proving that the release has no other discs.

ROMHarbor queries the enabled sources in parallel, each one under its own time limit. The order of
the source list is the priority order: when two sources report the same game, the one listed first
wins. When a source does not answer, the catalog shows the games of the other sources and names the
source that failed.

ROMHarbor does not include ROMs, BIOS files, keys, an emulator, or device-transfer tools.
Only download content you are authorized to use. It is an acquisition tool, not a replacement
for a library gallery such as RomM.

## Installation

| Method | Best for | Platforms |
| --- | --- | --- |
| [Container image](#container-image) | Running a released version with Docker | Linux AMD64 |
| [Docker Compose](#docker-compose) | Persistent, repeatable container setup | Linux AMD64 with the released image |
| [Nix / NixOS](#nix--nixos) | Reproducible package or declarative system service | Linux x86_64 and ARM64 |
| [From source](#from-source) | Development or a custom build | Go 1.25+, CGO, and a C compiler |

### Container image

The published image is available at `ghcr.io/glats/romharbor:0.1.0`. No registry login is needed.

```sh
docker run -d --name romharbor --stop-timeout 35 \
  -p 127.0.0.1:8080:8080 \
  --mount type=volume,source=romharbor-data,target=/data \
  ghcr.io/glats/romharbor:0.1.0

docker run -d --name romharbor-worker --stop-timeout 35 \
  --entrypoint /app/worker \
  --mount type=volume,source=romharbor-data,target=/data \
  ghcr.io/glats/romharbor:0.1.0
```

Open **http://localhost:8080** for the catalog. The named volume keeps your database, listing cache,
and downloaded games across container replacements. Do not delete it when updating.

The web container serves the interface and queues the downloads; the worker container runs them.
Both share the same volume and the same database. Without the worker, queued downloads never start.

The image includes CA certificates and runs as UID/GID `10001:10001`.
Existing volumes or bind mounts must be writable by that identity. Pin a version or digest;
the release workflow does not publish a `latest` tag.

### Docker Compose

```sh
git clone --branch v0.1.0 https://github.com/glats/romharbor.git
cd romharbor
export ROMHARBOR_IMAGE=ghcr.io/glats/romharbor:0.1.0
docker compose pull
docker compose up --no-build -d
```

Compose uses the same persistent volume and loopback-only port, with a read-only root filesystem
and restricted container privileges. It starts two services from the same image: `romharbor` serves
the web interface, and `romharbor-worker` runs the queued downloads. To build a local image instead,
omit `ROMHARBOR_IMAGE` and run `docker compose up --build -d` from a checkout.

Podman can build the Dockerfile, but its runtime behavior has not been verified.

### Nix / NixOS

Run the released package without installing it into a profile:

```sh
nix run github:glats/romharbor/v0.1.0 -- --listen 127.0.0.1:8080
```

Its database, cache, and library default to `./data/` in the working directory.
Nix packages are built in CI on native x86_64 and ARM64 Linux runners; Darwin is not a flake target.

For a declarative NixOS service, add `github:glats/romharbor/v0.1.0` to your flake inputs,
import its `nixosModules.default`, and set `services.romharbor.enable = true`.
The module provides options for the listen address, library location, cache, database, sources,
and service identity. It starts two units: `romharbor.service` serves the web interface and
`romharbor-worker.service` runs the queued downloads.
See the [module example and option reference](docs/distribution.md#nixos-module).

### From source

Install Go 1.25 or newer, and a C compiler. SQLite requires CGO;
templates and static assets are embedded, so no Node.js build or external database is needed.

```sh
git clone https://github.com/glats/romharbor.git
cd romharbor
make dev-bootstrap
make dev
```

Open **http://localhost:8080**. `make dev-bootstrap` creates `.env` from `env.dist` without replacing
an existing file. `make dev` starts the web server and the download worker together; stopping it
stops both. For a standalone executable, run `go build -o romharbor ./cmd/web` and
`go build -o romharbor-worker ./cmd/worker`.

## Configuration and storage

| Variable | Default outside containers | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Listen address |
| `ROM_LIBRARY_PATH` | `./data/roms` | Downloaded games, grouped by platform |
| `ROM_CACHE_PATH` | `./data/romcache` | Cached source listings |
| `ROM_SOURCES` | `archive.org` | Comma-separated enabled sources, in priority order |
| `ROM_CACHE_TTL` | `24h` | Listing cache freshness |
| `DATABASE_PATH` | `./data/app.db` | SQLite database |
| `JOBS_MAX_ATTEMPTS` | `3` | Attempts per download before it fails (worker) |
| `JOBS_RETRY_DELAY` | `30s` | Wait before the first retry; later retries wait four times as long (worker) |
| `JOBS_RETENTION` | `720h` | How long finished downloads stay listed (worker) |
| `JOBS_TIMEOUT` | `6h` | Longest a single download may run (worker) |

The flags `--listen`, `--library`, `--cache`, `--sources`, and `--ttl` override the corresponding
environment settings for one run. The download worker reads `DATABASE_PATH`, `ROM_LIBRARY_PATH` and the `JOBS_*` settings, and its `--database` and `--library` flags override them.
Container storage is configured under `/data`.
See [`env.dist`](env.dist) for logging and other application settings.

Back up persistent storage before upgrades, and run only one instance against a library.
For shared storage, provision writable directories and mount them before starting the service.

Upgrades must start the download worker as well. A queued download only leaves the queue when a
worker claims it, so an installation without a running worker queues downloads that never start.

**Keep ROMHarbor behind a trusted authenticated reverse proxy before exposing it beyond localhost.**
It does not configure authentication or TLS for you. The inherited `/users` example is still present;
cross-origin write protection is not authentication.

See [installation and releases](docs/distribution.md) for storage permissions, service options,
upgrades, container builds, and publishing details.

## Project structure

One Go application, with commands for actions and queries for reads:

```text
cmd/web/             Application startup, infrastructure wiring, and HTTP server
cmd/worker/          Download worker startup: claims the queued downloads and runs them
internal/domain/     ROM and user domain types
internal/app/        Command/query handlers and catalog coordination
internal/adapter/    Source providers, listing cache, download jobs, library, and SQLite
internal/port/web/   HTTP handlers, embedded templates, and static assets
internal/service/    Application assembly
nix/                 Package, NixOS module, and integration checks
tests/               Container smoke test
.github/workflows/   Verification and versioned GHCR publishing
```

HTTP handlers use `Application.Command` and `Application.Query`; concrete adapters are wired at startup.
`internal/adapter/config` loads environment configuration. `cmd/web` uses those values to construct
adapters and catalog collaborators and serves the HTTP interface; it only queues downloads.
`cmd/worker` uses the same configuration to claim and run the queued downloads from the same
database. The single `service.NewApp` receives those dependencies and only composes handlers;
it does not load configuration or own cleanup.
See [AGENTS.md](AGENTS.md) for contribution conventions and dependency boundaries.

For development checks, use `make test`, `make lint`, and `go test -race ./...`.

Provider API contracts live in `contract/openapi/` and are maintained by ROMHarbor, not the upstream
providers. Run `make generate-providers` after changing them and `make check-providers` to check
generated clients without changing files. Handwritten adapters map API responses or goquery-parsed
HTML to domain games; command/query handlers remain independent of those formats.
See [provider contracts](docs/provider-contracts.md) for generation and compatibility checks.

## License

[MIT](LICENSE). Vendored assets retain their own [third-party notices](internal/port/web/static/THIRD_PARTY.md).
ROMHarbor began from [glats/web-base](https://github.com/glats/web-base);
[ADR 0001](docs/adr/0001-web-base-decisions.md) records the foundation decisions.
