# ROMHarbor

A self-hosted web application for browsing ROM catalogs, finding games, and downloading them
into a library organized by platform. Run it on your own server and keep your library on your own storage.

## What it does

- Browse platforms and search their game listings.
- View game details, available files, and checksums when provided by the source.
- Start downloads and follow their progress in the web interface.
- Save games into platform folders in your library, ready for your own library manager or emulator.
- Cache listings locally to avoid fetching them on every visit.

Archive.org is the default source. `dlarchive` and `lolroms` are opt-in; `lolroms` requires search text.
Source availability and metadata depend on the upstream provider.

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
```

Open **http://localhost:8080** for the catalog. The named volume keeps your database, listing cache,
and downloaded games across container replacements. Do not delete it when updating.

The image includes `7z` and CA certificates and runs as UID/GID `10001:10001`.
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
and restricted container privileges. To build a local image instead, omit `ROMHARBOR_IMAGE` and run
`docker compose up --build -d` from a checkout.

Podman can build the Dockerfile, but its runtime behavior has not been verified.

### Nix / NixOS

Run the released package without installing it into a profile:

```sh
nix run github:glats/romharbor/v0.1.0 -- --listen 127.0.0.1:8080
```

The package includes `7z`. Its database, cache, and library default to `./data/` in the working directory.
Nix packages are built in CI on native x86_64 and ARM64 Linux runners; Darwin is not a flake target.

For a declarative NixOS service, add `github:glats/romharbor/v0.1.0` to your flake inputs,
import its `nixosModules.default`, and set `services.romharbor.enable = true`.
The module provides options for the listen address, library location, cache, database, sources,
and service identity. See the [module example and option reference](docs/distribution.md#nixos-module).

### From source

Install Go 1.25 or newer, a C compiler, and `7z` for `.7z` extraction. SQLite requires CGO;
templates and static assets are embedded, so no Node.js build or external database is needed.

```sh
git clone https://github.com/glats/romharbor.git
cd romharbor
make dev-bootstrap
make dev
```

Open **http://localhost:8080**. `make dev-bootstrap` creates `.env` from `env.dist` without replacing
an existing file. For a standalone executable, run `go build -o romharbor ./cmd/web`.

## Configuration and storage

| Variable | Default outside containers | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Listen address |
| `ROM_LIBRARY_PATH` | `./data/roms` | Downloaded games, grouped by platform |
| `ROM_CACHE_PATH` | `./data/romcache` | Cached source listings |
| `ROM_SOURCES` | `archive.org` | Comma-separated enabled sources |
| `ROM_CACHE_TTL` | `24h` | Listing cache freshness |
| `DATABASE_PATH` | `./data/app.db` | SQLite database |

The flags `--listen`, `--library`, `--cache`, `--sources`, and `--ttl` override the corresponding
environment settings for one run. Container storage is configured under `/data`.
See [`env.dist`](env.dist) for logging and other application settings.

Back up persistent storage before upgrades, and run only one instance against a library.
For shared storage, provision writable directories and mount them before starting the service.

**Keep ROMHarbor behind a trusted authenticated reverse proxy before exposing it beyond localhost.**
It does not configure authentication or TLS for you. The inherited `/users` example is still present;
cross-origin write protection is not authentication.

See [installation and releases](docs/distribution.md) for storage permissions, service options,
upgrades, container builds, and publishing details.

## Project structure

One Go application, with commands for actions and queries for reads:

```text
cmd/web/             Application startup, infrastructure wiring, and HTTP server
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
See [AGENTS.md](AGENTS.md) for contribution conventions and dependency boundaries.

For development checks, use `make test`, `make lint`, and `go test -race ./...`.

## License

[MIT](LICENSE). Vendored assets retain their own [third-party notices](internal/port/web/static/THIRD_PARTY.md).
ROMHarbor began from [glats/web-base](https://github.com/glats/web-base);
[ADR 0001](docs/adr/0001-web-base-decisions.md) records the foundation decisions.
