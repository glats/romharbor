package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/stephenafamo/bob"
	bobsqlite "github.com/stephenafamo/bob/dialect/sqlite"
	"github.com/stephenafamo/bob/dialect/sqlite/sm"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/sqlite/models"
	"app.ddd/internal/domain/rom"
)

type JobStore struct {
	db  bob.Executor
	Now func() time.Time
}

var (
	_ adapter.RomJobStore = (*JobStore)(nil)
	_ adapter.RomJobQueue = (*JobStore)(nil)
)

func NewJobStore(db bob.Executor) *JobStore {
	return &JobStore{db: db, Now: time.Now}
}

type storedRequest struct {
	Game rom.Game
	File rom.File
}

func (s *JobStore) Enqueue(ctx context.Context, req rom.InstallRequest) (rom.Job, error) {
	payload, err := json.Marshal(storedRequest{Game: req.Game, File: req.File})
	if err != nil {
		return rom.Job{}, fmt.Errorf("encode job request: %w", err)
	}
	at := s.Now()
	now := at.UnixMilli()

	for range 3 {
		var id int64
		err := s.scanOne(ctx, []any{&id},
			`INSERT INTO jobs (game_id, title, status, total, request, run_at, started)
			 VALUES (?, ?, 'queued', ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING
			 RETURNING id`,
			req.Game.ID, req.Game.Title, max(req.File.Size, 0), string(payload), now, at.UnixNano(),
		)
		if err == nil {
			return rom.Job{
				ID:      strconv.FormatInt(id, 10),
				GameID:  req.Game.ID,
				Title:   req.Game.Title,
				Status:  rom.JobQueued,
				Total:   max(req.File.Size, 0),
				Started: time.Unix(0, at.UnixNano()),
			}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return rom.Job{}, fmt.Errorf("enqueue job: %w", err)
		}

		var active int64
		err = s.scanOne(ctx, []any{&active},
			`SELECT id FROM jobs WHERE game_id = ? AND status IN ('queued', 'running')`, req.Game.ID,
		)
		if err == nil {
			return s.mustGet(ctx, active)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return rom.Job{}, fmt.Errorf("find active job: %w", err)
		}
	}
	return rom.Job{}, errors.New("enqueue job: the active job kept changing")
}

func (s *JobStore) Claim(ctx context.Context, lease time.Duration) (rom.JobClaim, bool, error) {
	for {
		now := s.Now()
		var (
			id       int64
			request  string
			attempts int
		)
		err := s.scanOne(ctx, []any{&id, &request, &attempts},
			`UPDATE jobs
			 SET status = 'running', attempts = attempts + 1, lease_until = ?, error = ''
			 WHERE id = (
			     SELECT id FROM jobs WHERE status = 'queued' AND run_at <= ? ORDER BY run_at, id LIMIT 1
			 )
			 RETURNING id, request, attempts`,
			now.Add(lease).UnixMilli(), now.UnixMilli(),
		)
		if errors.Is(err, sql.ErrNoRows) {
			return rom.JobClaim{}, false, nil
		}
		if err != nil {
			return rom.JobClaim{}, false, fmt.Errorf("claim job: %w", err)
		}

		job, err := s.mustGet(ctx, id)
		if err != nil {
			return rom.JobClaim{}, false, err
		}
		install, err := decodeRequest(request)
		if err != nil {
			if failErr := s.Fail(ctx, job.ID, err); failErr != nil {
				return rom.JobClaim{}, false, failErr
			}
			continue
		}
		return rom.JobClaim{Job: job, Request: install, Attempt: attempts}, true, nil
	}
}

func (s *JobStore) scanOne(ctx context.Context, dest []any, query string, args ...any) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Close()
}

func decodeRequest(payload string) (rom.InstallRequest, error) {
	var stored storedRequest
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return rom.InstallRequest{}, fmt.Errorf("decode stored job request: %w", err)
	}
	platform, ok := rom.PlatformBySlug(stored.Game.Platform)
	if !ok {
		return rom.InstallRequest{}, rom.ErrUnknownPlatform
	}
	return rom.InstallRequest{Game: stored.Game, File: stored.File, Platform: platform}, nil
}

func (s *JobStore) Heartbeat(ctx context.Context, id string, downloaded, total int64, lease time.Duration) error {
	return s.updateRunning(ctx, id,
		`UPDATE jobs SET downloaded = ?, total = CASE WHEN ? > 0 THEN ? ELSE total END, lease_until = ?
		 WHERE id = ? AND status = 'running'`,
		downloaded, total, total, s.Now().Add(lease).UnixMilli())
}

func (s *JobStore) Complete(ctx context.Context, id string, result rom.InstallResult) error {
	return s.updateRunning(ctx, id,
		`UPDATE jobs SET status = 'done', path = ?, total = ?, error = '', lease_until = 0, finished = ?
		 WHERE id = ? AND status = 'running'`,
		result.Path, result.Size, s.Now().UnixNano())
}

func (s *JobStore) Retry(ctx context.Context, id string, cause error, runAt time.Time) error {
	return s.updateRunning(ctx, id,
		`UPDATE jobs SET status = 'queued', error = ?, run_at = ?, lease_until = 0
		 WHERE id = ? AND status = 'running'`,
		cause.Error(), runAt.UnixMilli())
}

func (s *JobStore) Fail(ctx context.Context, id string, cause error) error {
	return s.updateRunning(ctx, id,
		`UPDATE jobs SET status = 'failed', error = ?, lease_until = 0, finished = ?
		 WHERE id = ? AND status IN ('queued', 'running')`,
		cause.Error(), s.Now().UnixNano())
}

func (s *JobStore) Release(ctx context.Context, id string) error {
	return s.updateRunning(ctx, id,
		`UPDATE jobs SET status = 'queued', attempts = MAX(attempts - 1, 0), run_at = ?, lease_until = 0
		 WHERE id = ? AND status = 'running'`,
		s.Now().UnixMilli())
}

func (s *JobStore) Recover(ctx context.Context) (int, error) {
	now := s.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'queued', run_at = ?, lease_until = 0
		 WHERE status = 'running' AND lease_until < ?`, now, now)
	return affected(res, err, "recover jobs")
}

func (s *JobStore) Prune(ctx context.Context, finishedBefore time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM jobs WHERE status IN ('done', 'failed') AND finished < ?`, finishedBefore.UnixNano())
	return affected(res, err, "prune jobs")
}

func (s *JobStore) Get(ctx context.Context, id string) (rom.Job, bool, error) {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return rom.Job{}, false, nil
	}
	row, err := models.Jobs.Query(sm.Where(models.Jobs.Columns.ID.EQ(bobsqlite.Arg(number)))).One(ctx, s.db)
	if errors.Is(err, sql.ErrNoRows) {
		return rom.Job{}, false, nil
	}
	if err != nil {
		return rom.Job{}, false, fmt.Errorf("get job: %w", err)
	}
	return toJob(row), true, nil
}

func (s *JobStore) List(ctx context.Context) ([]rom.Job, error) {
	rows, err := models.Jobs.Query(sm.OrderBy(models.Jobs.Columns.ID).Desc()).All(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	jobs := make([]rom.Job, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, toJob(row))
	}
	return jobs, nil
}

func (s *JobStore) mustGet(ctx context.Context, id int64) (rom.Job, error) {
	job, ok, err := s.Get(ctx, strconv.FormatInt(id, 10))
	if err != nil {
		return rom.Job{}, err
	}
	if !ok {
		return rom.Job{}, rom.ErrJobNotFound
	}
	return job, nil
}

func (s *JobStore) updateRunning(ctx context.Context, id, statement string, args ...any) error {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("update job %s: %w", id, rom.ErrJobNotFound)
	}
	res, err := s.db.ExecContext(ctx, statement, append(args, number)...)
	rows, err := affected(res, err, fmt.Sprintf("update job %s", id))
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("update job %s: %w", id, rom.ErrLeaseLost)
	}
	return nil
}

func affected(res sql.Result, err error, what string) (int, error) {
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return int(n), nil
}

func toJob(row *models.Job) rom.Job {
	job := rom.Job{
		ID:         strconv.FormatInt(row.ID, 10),
		GameID:     row.GameID,
		Title:      row.Title,
		Status:     row.Status,
		Downloaded: row.Downloaded,
		Total:      row.Total,
		Error:      row.Error,
		Path:       row.Path,
		Started:    time.Unix(0, row.Started),
	}
	if row.Finished > 0 {
		job.Finished = time.Unix(0, row.Finished)
	}
	return job
}
