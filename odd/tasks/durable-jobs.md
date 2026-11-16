# Durable download jobs
Replace the in-memory job store and bare download goroutines with a persisted queue inside the existing SQLite database.
Baseline: `7464229`; branch `feat/durable-jobs`. Delivery: local work-unit commits only; push, tag, release and NixOS pin need separate authorization. Runner: `make test`, `make lint`, `go vet ./...`, `go test -race ./...`.

## Specs

S1. User: "encuentor muy flaky esa solcuion a epsar de que la veo bien." Jobs are persisted in a SQLite `jobs` table created by a dbmate migration and survive a restart. Job IDs come from the store, not from a counter in the web handler; `GetQueued` and the handler `jobSeq` disappear.
S2. User: "si" to the proposal: "tabla `jobs` en tu SQLite, worker pool acotado con leases, recuperación al arrancar". Downloads run through a bounded worker pool. A worker claims one queued job atomically (`UPDATE ... RETURNING`) with a lease that progress updates renew. At startup, and periodically, jobs whose lease expired return to `queued`; the `.part` file and HTTP Range resume the transfer. Failed attempts retry with backoff up to a limit, then end `failed`. Starting a game that already has a queued or running job returns that job instead of creating a second one (partial unique index). A shutdown releases the running job back to `queued` without counting a failure.
S3. User: "lo de sse no puede ser porque tiene limitancias." No SSE, WebSocket, broker or PostgreSQL. Progress stays HTTP polling made cheap with `ETag` / `If-None-Match` so unchanged state answers `304`.
S4. Preserve the public HTTP shapes: `POST /api/download` answers `202` with the job JSON, `GET /api/job?id=` and `GET /api/jobs` keep their fields (`id`, `game_id`, `title`, `status`, `downloaded`, `total`, `error`, `path`, `started`, `finished`) and statuses `queued`, `running`, `done`, `failed`. Every use case stays behind `Application.Command` / `Application.Query`; adapters hide the database.
S5. Proof uses real temporary SQLite databases, synthetic files and no live downloads: restart recovery, claim exclusivity under concurrency (race detector), dedup, retry/backoff, shutdown release, throttled progress writes, conditional polling.

Assumptions (mine, adjustable): 1 worker by default (the library serializes installs, see L4), no new env variable; 3 attempts with 30s then 2m backoff; lease 30s renewed at most every 10s; progress persisted at most once per second.

## Tasks

- T1 [S1,S2] complete — inline: migration, regenerated bob models, SQLite job store. RED: store tests did not compile before `JobStore`; GREEN: 13 store tests incl. 24 concurrent claims under `-race`. Commit: with T2 (the port change breaks the build on its own).
- T2 [S2,S4] complete — inline: ports, worker pool in `romdownloads`, command/query/web rewiring, `cmd/web`, removal of `romjobs`. RED: worker tests did not compile against the old store. GREEN: 10 worker tests; `-race -count=15` on store, worker, app and web packages (3330 tests). Commit: see the work-unit commit.
- T3 [S3,S4] pending — inline: ETag conditional polling on `/api/job` and `/api/jobs`. Commit: pending.
- T4 [S1-S5] pending — seam checks, differential against the baseline HTTP shapes, independent verifier (Risk item 4, concurrency/ordering), native review only through candidate consent. Commit: pending.

Route declaration: all inline; the units depend on each other (store port feeds the worker pool feeds the handlers), so no parallel-writer reason holds.

## Log

- L1: Original user request: "dale. ahora me quiero ir por los jobs. encuentor muy flaky esa solcuion a epsar de que la veo bien. porque no reivsar Work/fal a ver si encunetras un patorn. hay algo con pubsub y web socket. pero creo que es overfill. quizas usar sqlite o movernos a postgersql que tiene pubsub. no se. veamos que me orfece.s"
- L2: User: "lo de sse no puede ser porque tiene limitancias." Research (primary sources) found River needs Go 1.26 and marks SQLite experimental; goqite lacks dedup and progress; asynq needs Redis; LISTEN/NOTIFY adds nothing for one instance. The private reference repositories contain no job queue or progress push; only the idea of persisting progress with a context that survives cancellation was kept. Bundled SQLite is 3.53.4, past the WAL-reset fix in 3.51.3.
- L3: User approved the design with "si". Implementation authorized; publication not.
- L4: `Library.Install` already serializes installs with a capacity-1 gate (`internal/adapter/romlibrary/library.go:39-45`), so a second worker would only mark jobs `running` while they wait. The pool defaults to one worker and the size is a code option, not an env variable. Persisting jobs makes the list grow without bound (it used to reset on restart), so terminal jobs older than 30 days are pruned at startup; this is a consequence of S1, not a new feature.
- L5: Repeating the suite 15 times found two real defects, both fixed with a test first: (a) a claim lost to a cancelled lifecycle context left a job `running` after shutdown; `Claim` and outcome writes now use a context detached from cancellation. (b) recovery only ran at startup, so a restart inside the lease window orphaned the job; idle workers now also recover expired leases. Preserved behavior checked by existing tests: `202` carries the queued snapshot (`Enqueue` returns the inserted job, not a re-read), final progress is flushed before completion, checksum mismatch now fails only after the retry limit (consequence of S2). The database already lives on the persistent volume (`/data`, systemd `StateDirectory`).
