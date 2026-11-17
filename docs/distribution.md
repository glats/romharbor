# Installation and releases

ROMHarbor is self-hosted. The public repository contains application code and generic installation
options, not ROMs, private host configuration, credentials or an operated download service.
You supply the machine, persistent storage and any reverse proxy or authentication.

## Docker and Podman

From a checkout containing these packaging files:

```sh
docker compose up --build -d
```

The default image name is local (`romharbor:local`); no published image is assumed. Compose publishes
only `127.0.0.1:8080` and stores the SQLite database, cache and library in the named volume
`romharbor-data`. The process runs as UID/GID `10001:10001`, with a read-only root filesystem.
The image includes trusted CA certificates. Do not delete the volume during upgrades.

To build and run without Compose, including with Podman, use the Dockerfile and the equivalent
command in the main README, replacing `docker` with `podman` where appropriate. Podman is an
intended OCI-compatible path; do not infer runtime verification from the Docker tests.

After a versioned public image actually exists, replace `X.Y.Z` with that released version:

```sh
export ROMHARBOR_IMAGE=ghcr.io/glats/romharbor:X.Y.Z
docker compose pull
docker compose up --no-build -d
```

Back up the persistent volume before updating. Pin a version or image digest rather than `latest`.
Do not run multiple ROMHarbor instances against one library: install serialization is instance-local.
For a host bind mount instead of the named volume, create the directories and grant UID 10001 access
yourself; ROMHarbor does not change ownership of your existing library.

The initial publishing workflow targets `linux/amd64` only. ARM64 container releases are not
claimed. Nix provides separate Linux architecture outputs below.

## Nix package

The flake locks nixpkgs to the `nixos-26.05` channel. Its Go 1.25 builder supports the application's
declared Go version; `.7z` archives are read in-process, so the package needs no external extractor. No global profile install is required.

```sh
nix build .#romharbor
nix run .# -- --help
```

Outputs are declared for `x86_64-linux` and `aarch64-linux`. CI builds them on native runners.
An evaluation is not a runtime test, and Darwin is not a supported flake target in this delivery.
The application can still be built using the ordinary Go instructions.

## NixOS module

Add ROMHarbor as an input to your own configuration and import `romharbor.nixosModules.default`.
Use a released tag or a specific commit containing the flake; replace `REVISION` in this example.
Keep the consuming configuration's lock file so updates are explicit.

```nix
{
  inputs.romharbor.url = "github:glats/romharbor/REVISION";

  outputs = { nixpkgs, romharbor, ... }: {
    nixosConfigurations.server = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        romharbor.nixosModules.default
        { services.romharbor.enable = true; }
      ];
    };
  };
}
```

This is an integration example, not an activation performed by ROMHarbor. Keep your existing
nixpkgs input and host configuration. The module defaults are:

| Option under `services.romharbor` | Default |
| --- | --- |
| `enable` | `false` |
| `package` | This flake's package for the host system |
| `address` | `127.0.0.1:8080` |
| `databasePath` | `/var/lib/romharbor/app.db` |
| `cachePath` | `/var/lib/romharbor/cache` |
| `libraryPath` | `/var/lib/romharbor/roms` |
| `sources` | `[ "archive.org" ]` |
| `cacheTtl` | `24h` |
| `user`, `group` | `romharbor` |

The module creates the default system user/group and persistent state directory. A custom user/group
must already exist. For external storage, pre-create directories writable by that identity; the
module does not take ownership of an existing library. Storage paths must be absolute. The service
sandbox protects home directories, so use a location such as `/srv` or a mounted media directory,
not a directory under `/home`. External mounts must be available when the service starts.

No firewall port, domain or authentication provider is configured automatically. Keep the default
loopback binding unless a trusted authenticated reverse proxy is configured. The inherited `/users`
example is still present; do not expose an unauthenticated installation to the public Internet.

## Verification and publishing

CI verifies Go tests/lint/race, a non-publishing Docker build with persistent-state smoke tests,
and native Nix builds/module evaluations on both Linux architectures. Module checks evaluate
defaults, overrides and sandbox configuration; they are not an activated NixOS VM test.
An additional `checks.x86_64-linux.service` test boots an isolated NixOS VM when KVM is available,
checks the real service and its sandbox, rejects cache traversal, and verifies SQLite persistence
across service restart. It never contacts ROM sources or changes the host's active system.

```sh
make test
make lint
go test -race ./...
docker compose config --quiet
docker build --tag romharbor:test .
sh tests/container-smoke.sh romharbor:test
nix flake check --no-build
nix build --no-link .#default
nix build --no-link .#checks.x86_64-linux.service
```

The smoke test uses synthetic local user data, never upstream listings or ROM payloads. It rejects
an invalid-platform request with HTTP 400 without touching a neighboring JSON file. Valid platform
and empty-platform API requests retain their prior behavior.

The release workflow runs only when a `v`-prefixed version tag is pushed. It first reuses CI and then
publishes to `ghcr.io/glats/romharbor` with version, minor-version and commit tags using `GITHUB_TOKEN`.
Only the publishing job receives `packages: write`; PR jobs have read-only repository permissions.
External actions are pinned to commits. No Docker Hub account or personal access token is needed.

Creating a tag, triggering publication and making a GHCR package public are explicit maintainer
actions. After the first publication, check the package visibility and verify an anonymous pull;
do not assume a public repository made the image public. GitHub warns that making a package public
cannot be reversed to private. This delivery does not create a tag, image, release or visibility change.

GitHub currently provides [free Container registry storage and bandwidth](https://docs.github.com/en/billing/concepts/product-billing/github-packages)
and [free standard Actions runners for public repositories](https://docs.github.com/en/actions/concepts/billing-and-usage).
These policies can change; they do not provide free application hosting or storage for your library.
