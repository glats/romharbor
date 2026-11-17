# Adapter audit
Inventory every adapter defect once, classify it, and close it against one ledger instead of finding defects one at a time.
Baseline: `3916b5d` (v0.1.3); branch `feat/adapter-audit`. Delivery: local work-unit commits only; push, tag, release and NixOS pin need separate authorization. Runner: `make test`, `make lint`, `go vet ./...`, `go test -race ./...`.

## Specs

S1. User: "yo creo que tnemos que identificar todo lo que esta mal hecho. en adapter. aun veo http directas y no usamos clientes con codegen. entonces que sugieres para ir identificar todo e ir sin goteo? es harto entonces. de hecho yo iria por odd.project incluso". One complete inventory of the adapter layer judged against explicit criteria; the ledger below is the whole scope.
S2. A finding that is not in the ledger is a new finding: it is recorded here and triaged, never fixed on the side. Regressions caused by a correction are the only exception.
S3. Rules before corrections. Whatever a lint or static rule can express lands first as a deterministic check that fails on the rejected pattern and passes on the base; rule files are changed only after user approval. Order of preference: lint rule, test, architecture change, prose.
S4. The user decides the open decisions (D1..D4 below) one at a time. Technical verification does not stand for user acceptance.
S5. Closure: every ledger item is closed, deferred with a reason, or rejected by the user; `make lint`, `make test`, `go test -race ./...` and `make check-providers` pass; no new unclassified finding remains open.

Criteria used by the audit (derived from the user's corrections and AGENTS.md): C1 transport not through a generated client or a hidden transport; C2 business rules inside adapters; C3 leakage and surface that exists only for tests or wiring; C4 errors and resources; C5 persistence consistency; C6 dead or compatibility code; C7 concurrency and lifecycle; C8 naming and layout.

## Tasks

- T0 [S1] complete — read-only audit by four explorers (romsource; romlibrary; sqlite, romcache, config and composition root; romlistings, romdownloads, ports, handlers and web). 99 findings: 2 blocker, 41 major, 56 minor. Consolidated below. Commit: with this document.
- T1 [S4] in progress (D1, D2, D3 closed) — close the open decisions D1..D4 one at a time and record each answer verbatim in the Log.
- T2 [S3] pending — checks first: lint and depguard rules for the patterns the ledger classes as rule-expressible, each proven to fail on a reproduction and pass on the base. Rule files need user approval.
- T3 [S5] pending — corrections by cluster, in dependency order (K1 transport, K2 errors and install integrity, K3 ports and boundaries, K4 state and memory, K5 persistence, K6 compatibility code, K7 surface, K8 composition root). Each cluster is expanded only when selected.
- T4 [S5] pending — closure proof and independent verification for clusters that touch concurrency, persisted formats or contracts others consume.

Route: all inline unless a cluster names a writer reason; no unit is expanded before its selection.

## Ledger

Severity: B blocker, M major, m minor. Fix: lint, test, arch (architecture change). Evidence is `path:line` at `3916b5d`; "unverified" is the auditor's own mark and is carried here.

### K1 Transport and generated clients (C1)
- R-01 M lolroms.go:97-107: hand-built HTTP request for the HTML search; no client seam. arch.
- R-02 M archiveorg.go:180-190, dlarchive.go:177-185: generated client only builds the request; status, body and JSON decoding are hand-written; typed responses unused. arch.
- R-27 M contract yaml, adapters: contract unions (list/file-url/error, metadata/empty array) are bypassed; error payload type unused. arch.
- R-13 M archiveorg.go:189, dlarchive.go:185: JSON decoding without a size bound (the HTML path is bounded). arch.
- R-14 M archiveorg.go:30-32, dlarchive.go:33-35, lolroms.go:29-31, main.go:190-194: nil client becomes `http.DefaultClient` (no timeout) and main passes nil. lint/test.
- L-01 B library.go:182-195: hand-written download request, `Range` header and body streaming; depguard allows all of `$gostd` there. arch + lint.
- L-02 B extract.go:90-92: runs `7z` through `exec`; output unbounded in memory and pasted into errors. arch + lint.
- L-19 m library.go:52: download URL goes from source JSON to the HTTP client with no scheme or host policy; redirects followed. test.
- L-12 M library.go:177-193, downloads.go:178, main.go:113: no idle or header timeout (`&http.Client{}`); a stalled server holds the gate up to the 6 h job timeout. test + arch.
- S-23 m main.go:113,116: `&http.Client{}` without timeout; third argument of `NewDownloads` is nil (unverified). lint.
- R-12 m lolroms.go:86-89: 8 MiB `LimitReader` truncates silently with no overflow error. test.

### K2 Errors and install integrity (C4)
- L-15 M library.go:195-201, downloads.go:218: every library error is a stringly `fmt.Errorf`; permanent failures (checksum mismatch, no ROM in archive, unsupported format, missing 7z) are retried with backoff. arch + test.
- L-06 M library.go:97, 242-257: downloaded byte count never compared with `File.Size`; an HTTP 200 error page without a published checksum is installed as a ROM. test + domain rule.
- L-05 M library.go:177-226: resume sends `Range` without `If-Range`/ETag and does not validate `Content-Range`; a 416 on a complete `.part` is discarded. test + arch.
- L-03 M extract.go:79, 68-87: zip entry copied without size, count or total cap and not cancellable. test + arch.
- L-04 M extract.go:96-118: extracted tree unbounded; `WalkDir` treats symlinks as files (7z default unverified). test.
- L-08 M library.go:59-60,77 vs platform.go:187: `arcade` lists `.zip`/`.7z` as ROM extensions but `.zip` is always unpacked, so the arcade install path looks unreachable (not run). test first to confirm.
- L-13 m library.go:91-130: `.part` and extract directory leak on several failure paths. test.
- L-14 m library.go:275-284, extract.go:75-86: no fsync of the extracted ROM or directory; two renames, failure on the second loses the download. test.
- L-10 m library.go:97-100, 242-273: up to three full re-reads for checksums, not cancellable; shutdown waits on them. arch + test.
- L-24 m library.go:228-240: progress ignores `Content-Length` and `Content-Range`. test.
- L-23 m extract.go:56-65,114-118: only one ROM per archive (.cue+.bin loses files). arch.
- A-14 M start_download.go:29-31: any `Game()` error becomes `ErrGameNotFound`, so infrastructure failures surface as 404. test.
- A-18 M web/catalog.go (11 sites): `err.Error()` sent to HTML and JSON, nothing logged, bypassing `h.fail` and the AGENTS.md rule. test.
- A-19 m catalog.go:113,184,160-164,84: any `ObtainGame` error becomes 404; paging defaults in the handler, no max limit, `Atoi` errors ignored. test.
- A-05 M listings.go:44,89-92,145-147: `PlatformCounts` silently drops failing platforms; a cache `Store` failure makes `Count`/`Games` discard the games. test.
- A-07 M registry.go:36-51: source errors dropped when any source returns games; first-wins merge makes configuration order an implicit priority; sources queried sequentially. arch.
- R-15 m archiveorg.go:82-85: an unparsable string size becomes 0 silently. test.
- R-16 m lolroms.go:135-146: `PathUnescape` errors swallowed with `continue`. test.
- S-03 M job_store.go:241-250: `updateRunning` returns nil for an unparsable id and ignores `RowsAffected`; a lost lease makes heartbeat, complete, retry and release succeed silently; a test encodes it. arch (port reports lease loss) + test.
- A-11 M downloads.go:239-241, job_store.go:244-248: heartbeat failure only logged; `Complete` is a no-op after lease loss; duplicate execution plausible (unverified). test.
- S-14 m romcache/cache.go:26-40, listings.go:126: raw `os`/`json` errors, no sentinel, caller discards them so a corrupt cache is ignored; port has no `ctx`. arch.
- S-24 m connection.go:14,28; migrate.go:39,52; seed.go:16: `Open`, `Migrate`, `Rollback` take no `ctx`; `Ping` unbounded. lint.

### K3 Ports and boundaries (C2, C6)
- A-04 M listings.go:54-83: merge, substring filter, paging, cover decoration and TTL/stale-on-error policy sit in the adapter; `time.Now` not injectable; no dedup of `listing + found`. arch.
- A-08 M repository.go:53-57: `PlatformCounts` has no error return; `Games` mixes browse and search with four positional arguments; `Game` returns `(T,bool,error)`; the port is stateful. arch.
- A-09 M repository.go:41-51, job_store.go:95,184,241-246: `RomJobQueue` has nine methods across producer, worker and maintenance; the state machine lives in SQL. arch.
- A-10 M downloads.go:15-28,68-73,218-220: retry, backoff, attempts, retention, lease and the 6 h timeout are adapter policy with exported mutable fields. arch.
- A-13 M start_download.go:13,44, catalog.go:221-224: `Queued func(Job)` callback returns data from a command. arch.
- A-15 M list_games.go:29, list_platforms.go:24, list_jobs.go:24, obtain_destination.go:26: pure forwarders; get-game and get-job only translate not-found. arch.
- A-16 M list_games.go:29 -> listings.go:66-73,144: "queries" write the in-memory cache and disk, against AGENTS.md; after a restart `StartDownload` of a game found only through search fails with not-found. arch.
- A-17 m repository.go:31-34, obtain_destination.go:17: `RomLibrary` mixes `PlatformDir` (read) and `Install` (write); a query depends on it and the page shows a filesystem path. arch.
- L-17 m library.go:30-36, repository.go:32: `PlatformDir` exposes filesystem layout through the port with no containment check; `InstallResult.Path` is an absolute host path persisted by the job store. arch.
- L-07 M library.go:55-83: extension policy is inconsistent (plain files with any extension install; empty extension falls back to `Extensions[0]`; archives checked through `PickROM`). arch (domain rule).
- L-09 m library.go:81-83: skip-if-present policy is in the adapter, compares size only, archives always re-download. arch.
- L-18 m library.go:134-141: path containment is lexical in the adapter; `Dir="."` installs into the root. arch + test.
- L-22 m game.go:97-101: `InstallRequest.Game` never read by the library; `File.Archive` only rejects. arch.
- R-10 M archiveorg.go:115-127, dlarchive.go:92-120, lolroms.go:165: text filter, title sort and pagination duplicated inside each adapter, the registry also dedups. arch.
- R-11 m dlarchive.go:56-69, lolroms.go:51-68, archiveorg.go:36-48: per-provider platform tables beside the domain `Platform.Queries`; two sources of platform truth. test (consistency).
- R-20 m lolroms.go:156 vs archiveorg.go:159, dlarchive.go:109: region detection on the raw name versus the cleaned title. test.
- L-20 m naming.go and platform.go: four duplicated extension lists/helpers drift (`IsArchive` vs `ArchiveExtension`, `HasROMExt` vs `Platform.Accepts`, missing `.cso .img .mds .gdi .nsz .vpkg .ngage`, `.smd` twice). test (drift).
- A-20 m catalog.go:59-68,89,122: the home page triggers a full catalog fetch just for `Total`; pages embed the whole job history. arch.
- A-23 m service/app.go:11-12, game.go:61-64, archiveorg.go:127: `NoOp` metrics threaded through every handler; `NewApp` takes five positional ports; `Query.Limit/Offset` never set by listings. prose.

### K4 State, memory and concurrency (C4, C7)
- A-01 M listings.go:24,71,96-101: `searchGames` map written on every text search, no eviction, preferred over fresh listings. arch.
- A-02 M listings.go:102-111: `Game(id)` loops over every platform fetching on cache miss, swallows per-platform errors, not-found is `false` with nil error. arch.
- A-03 M listings.go:120-131: check, fetch and store with no per-platform lock or singleflight; concurrent requests stampede the sources. test + arch.
- R-17 M dlarchive.go:125-150: mutex held across a multi-page network fetch; no singleflight; `ok:false` pages not checked; an empty index is never cached. test.
- R-18 m archiveorg.go:98-113: no cache; sequential per-item metadata fetches. arch.
- S-07 m job_store.go:218-228, user_repository.go:99-111: `List` without `LIMIT` in either repository. arch.
- S-12 M romcache/cache.go:54-58: fixed `path+".tmp"`, no fsync or cleanup; concurrent `Store` of one platform can rename a half-written file. test + small fix.
- L-11 M library.go:38-48,22-24: hidden global install gate of size 1 serialises installs; `Workers` in `romdownloads` is moot; gate acquired before validation. arch.
- A-12 m downloads.go:75-132, main.go:117,124,126: `Resume` and `Shutdown` are not on the port, wiring uses the concrete type; `Start` before `Resume` enqueues with no workers; `Shutdown` leaks its wait goroutine on timeout; main discards the error and calls it twice. test.
- S-08 m job_store.go:48,85-121: enqueue retry literal `3`; `Claim` makes three non-transactional round trips and loops after failing a corrupt row. test.

### K5 Persistence consistency (C5)
- S-01 M job_store.go (ten sites): ten raw SQL writes while `Get`/`List` and the user repository use the bob DSL. lint then arch.
- S-02 M job_store.go:51-56,94-100,184 and migration: state machine and "one active job per game" live in SQL; status literals duplicate `rom.JobQueued`...; `attempts = MAX(attempts-1,0)` in SQL. test then arch.
- S-05 m job_store.go (many) and migration: `started`/`finished` in nanoseconds, `run_at`/`lease_until` in milliseconds in one table; a test pins it. test or arch.
- S-06 M job_store.go:35-45,142-151, game.go:6-45: the `request` column stores untagged domain `Game`/`File` JSON, so Go field names are the persisted protocol; decode failure calls `Fail` inside `Claim`. test (golden) then arch (DTO).
- S-09 m connection.go:23-31, seed.go:29: no `SetMaxOpenConns`, `_txlock=immediate` or `synchronous`; `Seed` opens a deferred transaction; file mode unverified. test.
- S-10 m migrate.go:21,25-30, connection.go:19,36-41: two DSNs and drivers; `validatePath` papers over the difference; `MkdirAll` duplicated. arch.
- S-11 m user_repository.go:121: unique-violation mapping depends on the generated name `...UsersAutoindex2`. test (present).
- S-15 m romcache/cache.go:17-20, cache_test.go:14-49: the untagged domain struct is the on-disk cache format, locked by a test for compatibility. prose (decide).
- S-16 m romcache/cache.go:61-66: platform validation duplicated with `listings.go:117`; empty platform accepted. test.
- S-13 m romcache/cache.go:47,55: modes 0o755/0o644 against 0o750 elsewhere. lint (gosec).

### K6 Compatibility and dead code (C6)
- R-03 M provider_errors.go:11-64: rewrites decode errors into names of types that no longer exist; tests pin the strings. arch (delete) then test update.
- R-04 M provider_errors.go:53-64, archiveorg.go:144,190: re-parses every size after a failed decode to guess the original error; stringly typed. arch (with R-03).
- R-05 m docs/provider-contracts.md:41-48, archiveorg.yaml:23: documents behavior kept "as before" for byte compatibility. prose.
- R-09 m dlarchive.go:154-164: `FileURL` is exported, not on the port, only tests call it (grep limited to `internal/`). lint/delete.
- R-24 m lolroms.go:84: redundant `7Z` in the extension regex. lint.
- R-26 m contract yaml:12, archiveorg.go:35, dlarchive.go:38: `servers.url` and adapter default `BaseURL` duplicate each other. test.
- L-21 m library.go:33,50,53, naming.go:102,164: stale `romcatalog:` error prefix after the rename; domain emits adapter-prefixed errors. lint.
- R-25 m lolroms.go:101: `User-Agent` is "romcatalog/1.0", not the product name. prose.
- R-22 m archiveorg.go:156, dlarchive.go:106, lolroms.go:153: source literal repeated instead of `Name()`. lint (goconst).
- A-22 m catalog.go and game.go: duplicated `catalogPlatformCount`; default `slog` instead of `h.logger`; base64 id on the page route but raw id on the API; JSON tags in the domain. prose.

### K7 Surface and hidden capability (C3)
- R-06 M archiveorg.go:21-26, dlarchive.go:20-25, lolroms.go:22-25: exported mutable `HTTPClient`, `BaseURL`, `Sets`, `TTL`; tests mutate them. arch (constructor options).
- R-07 m archiveorg.go:176, dlarchive.go:167: a new generated client per call, needed only because `BaseURL` is mutable. arch (with R-06).
- R-08 / A-06 M lolroms.go:38, registry.go:31-33: `SearchOnly()` found by anonymous-interface type assertion; routing rule lives in the registry; sources without it never receive `Text` (production dead branch unverified). arch (explicit capability) or lint.
- L-16 m library.go:25-27,143-155: `SevenZipPath` set only by tests, `HTTPClient` always set in production so the default is dead; three exported fields and a `sync.Once` instead of a constructor. arch.
- S-04 m job_store.go:21-24,32: exported `Now` exists for tests that are in the same package. lint.
- A-21 m handler.go:68-72,83-85: nil-check on `ListPlatforms` toggles a fallback route that `NewApp` never triggers. lint/test.

### K8 Composition root and config (C7, C8)
- S-17 m config.go:51-58: `Get()` panics; only `main.go:42` uses it. lint (forbid `panic`).
- S-18 m config.go:35-40, main.go:185: `ROM_SOURCES` parsed in main; no validation for TTL, paths or sources; `godotenv.Load` outside config. arch.
- S-19 m main.go:49-55,108: `buildSources` runs twice; first result only validates and its fatals fire before the logger is configured. arch.
- S-20 m main.go:71-89: flags override five fields only; DB path, seed and log level have no flag. prose.
- S-21 m main.go:168-170: `ListPlatforms != nil` decides `WriteTimeout = 0`; intent unverified. test.
- S-22 m main.go:126,157: `stopDownloads` runs in a defer and explicitly; benign. prose.
- S-25 m .golangci.yml:90: depguard `adapter-rom` rule lists the nonexistent `romjobs`; no rule for `cmd/web`; nothing forbids `os.Getenv`, raw SQL, `net/http` or `os/exec` in the wrong packages. lint.
- R-19 m dlarchive.go:166-185,171-176: request editor used only to capture the URL for an error label that yields `files_api.php?action=...`. test.
- R-21 m lolroms.go:157-160,131-134: `BaseURL + href` assumes a path-absolute href, which is also the game ID (unverified). test.
- R-23 m dlarchive.go:47-53, lolroms.go:41-47: `Platforms()` returns map-iteration order; only archiveorg sorts. test.

### Systemic patterns
1. Generated clients are used as request builders only; everything after the request is hand-written (K1, K6).
2. Domain and application behavior lives in two adapters, so the handlers are empty shells (K3, K4).
3. Rules live in prose and tests that a lint rule could enforce; depguard allows all of `$gostd` in `romlibrary` and has no `cmd/web` rule (K1, K8).
4. Several stateful ports carry hidden temporal coupling and silent error drops (K2, K4).
5. Behavior kept for historical byte-compatibility outlives its reason (K6).

## Open decisions

- D1 HTTP policy — CLOSED (L6). Do LolRoms and the file download also go through a contract with a generated client? Facts from the audit: LolRoms fits (one GET with a query parameter and a `text/html` response); the download does not fit well (the generated client fixes its server at construction and re-escapes path parameters, `WithResponses` buffers the whole body, and `Range` resume, `Content-Range` validation and `.part` handling stay hand-written either way).
- D2 Browse and search — CLOSED (L7). Keep platform browsing and text search both, or always start from search? It shapes the behavior-named CQRS surface (K3).
- D3 Strictness for compatibility — CLOSED (L8). May K6 delete behavior kept only for historical byte-compatibility (error strings, legacy labels, the untagged cache format)?
- D4 Order and size. Which cluster first after the checks, and whether `7z` stays an external process behind a port or moves to a pure-Go library.

## Log

- L1: Original request: "yo creo que tnemos que identificar todo lo que esta mal hecho. en adapter. aun veo http directas y no usamos clientes con codegen. entonces que sugieres para ir identificar todo e ir sin goteo? es harto entonces. de hecho yo iria por odd.project incluso"
- L2: Context from earlier turns kept verbatim: "no hay catalog/ es command query/ etc."; "todo lo que sea json peude ser asi [contrato OpenAPI propio y cliente generado]"; "lo que es scareper podemos usar alguna lib"; "lo de sse no puede ser porque tiene limitancias."
- L3: Method (odd-project): inventory first against explicit criteria; one ledger as the only scope; rules as deterministic checks before corrections; one user triage; corrections by cluster with proof. No Gas Town campaign is started; the adapter layer is about 2.2k production lines and the units depend on each other.
- L4: Audit results: romsource 27, sqlite/romcache/config/composition root 25, romlibrary 24, romlistings/romdownloads/ports/handlers/web 23 findings, 99 in total (2 blocker, 41 major, 56 minor). Several auditor claims are marked unverified and carried as such (arcade install path, 7z symlinks, duplicate execution after lease loss, `FileURL` callers outside `internal/`).
- L5: My own earlier decisions are re-opened by this audit and not defended: `provider_errors.go` (R-03, R-04), accepting the missing lease-loss signal (S-03, A-11), and retrying permanent errors (L-15).
- L6: D1 answered by the user with the option "LolRoms con contrato; descarga confinada (Recomendada)": LolRoms moves to an OpenAPI contract with a generated client; the Archive.org and DlArchive clients are used completely (typed responses, contract errors); the file download stays hand-written but confined to one package behind a narrow port; a lint rule forbids `net/http` and `os/exec` outside the generated packages and that one.
- L7: D2 answered by the user with the option "Mantener ambos, con identidad estable (Recomendada)": platform browsing and text search both stay; the in-memory search map is removed and a game ID is always resolved against its source, so a download works after a restart; the search-only capability becomes explicit on the source port instead of a hidden type assertion.
- L8: D3 answered by the user with the option "Borrar los tres (Recomendada)": delete `provider_errors.go` and the tests that pin legacy error strings (errors become those of the generated client and the contract); the on-disk listing cache moves to its own tagged format and old cache files are ignored and fetched again once (cache only, no user data); a trailing slash in a base URL stops producing a double slash.
- L9: Research for D4 (verified with primary sources): `github.com/bodgit/sevenzip` v1.6.5 is a pure-Go 7-zip reader, BSD-3-Clause, `go 1.25.0`, active (252 stars, 15 open issues), with about 13 module dependencies, including `klauspost/compress`, `ulikunitz/xz`, `andybalholm/brotli` and `pierrec/lz4`; its README notes that more sample archives are needed to test every method combination. Adopting it would change `go.mod`/`go.sum` and therefore the Nix `vendorHash`, and would remove `p7zip` from the package wrapper.
