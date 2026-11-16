-- migrate:up
CREATE TABLE jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    game_id     TEXT    NOT NULL,
    title       TEXT    NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    downloaded  INTEGER NOT NULL DEFAULT 0,
    total       INTEGER NOT NULL DEFAULT 0,
    error       TEXT    NOT NULL DEFAULT '',
    path        TEXT    NOT NULL DEFAULT '',
    request     TEXT    NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    run_at      INTEGER NOT NULL,
    lease_until INTEGER NOT NULL DEFAULT 0,
    started     INTEGER NOT NULL,
    finished    INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX jobs_active_game ON jobs (game_id) WHERE status IN ('queued', 'running');
CREATE INDEX jobs_claim ON jobs (status, run_at);

-- migrate:down
DROP TABLE jobs;
