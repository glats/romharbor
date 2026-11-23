package romdownloads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

const (
	downloadTimeout = 6 * time.Hour
	persistTimeout  = 3 * time.Second
)

type Downloads struct {
	Workers       int
	Lease         time.Duration
	ProgressEvery time.Duration
	Poll          time.Duration
	MaxAttempts   int
	Backoff       func(attempt int) time.Duration
	Retention     time.Duration
	Now           func() time.Time

	library adapter.RomLibrary
	queue   adapter.RomJobQueue
	logger  *slog.Logger

	lifetime context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	workers  sync.WaitGroup

	mu      sync.Mutex
	resumed bool
}

var _ adapter.RomDownloads = (*Downloads)(nil)

func NewDownloads(library adapter.RomLibrary, queue adapter.RomJobQueue, logger *slog.Logger) *Downloads {
	if logger == nil {
		logger = slog.Default()
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Downloads{
		Workers:       1,
		Lease:         30 * time.Second,
		ProgressEvery: time.Second,
		Poll:          5 * time.Second,
		MaxAttempts:   3,
		Backoff:       defaultBackoff,
		Retention:     30 * 24 * time.Hour,
		Now:           time.Now,
		library:       library,
		queue:         queue,
		logger:        logger,
		lifetime:      lifetime,
		cancel:        cancel,
		wake:          make(chan struct{}, 1),
	}
}

func defaultBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func (d *Downloads) Resume(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lifetime.Err() != nil {
		return rom.ErrShuttingDown
	}
	if d.resumed {
		return nil
	}
	recovered, err := d.queue.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover interrupted downloads: %w", err)
	}
	if recovered > 0 {
		d.logger.Info("interrupted downloads queued again", "jobs", recovered)
	}
	if _, err := d.queue.Prune(ctx, d.Now().Add(-d.Retention)); err != nil {
		return fmt.Errorf("prune old downloads: %w", err)
	}
	d.resumed = true
	for range max(d.Workers, 1) {
		d.workers.Add(1)
		go d.work()
	}
	return nil
}

func (d *Downloads) Start(ctx context.Context, req rom.InstallRequest) (rom.Job, error) {
	if d.lifetime.Err() != nil {
		return rom.Job{}, rom.ErrShuttingDown
	}
	job, err := d.queue.Enqueue(ctx, req)
	if err != nil {
		return rom.Job{}, err
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return job, nil
}

func (d *Downloads) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	d.cancel()
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for downloads: %w", ctx.Err())
	}
}

func (d *Downloads) work() {
	defer d.workers.Done()
	for d.lifetime.Err() == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(d.lifetime), persistTimeout)
		claim, ok, err := d.queue.Claim(ctx, d.Lease)
		cancel()
		if err != nil {
			d.logger.Error("claim download", "error", err)
			d.idle()
			continue
		}
		if !ok {
			d.recover()
			d.idle()
			continue
		}
		d.run(claim)
	}
}

func (d *Downloads) recover() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(d.lifetime), persistTimeout)
	defer cancel()
	recovered, err := d.queue.Recover(ctx)
	if err != nil {
		d.logger.Error("recover downloads", "error", err)
		return
	}
	if recovered > 0 {
		d.logger.Info("downloads with an expired lease queued again", "jobs", recovered)
	}
}

func (d *Downloads) idle() {
	timer := time.NewTimer(d.Poll)
	defer timer.Stop()
	select {
	case <-d.wake:
	case <-timer.C:
	case <-d.lifetime.Done():
	}
}

func (d *Downloads) run(claim rom.JobClaim) {
	ctx, cancel := context.WithTimeout(d.lifetime, downloadTimeout)
	defer cancel()

	var downloaded, total atomic.Int64
	downloaded.Store(claim.Job.Downloaded)
	total.Store(claim.Job.Total)
	stopBeat := make(chan struct{})
	beatDone := make(chan struct{})
	var lost atomic.Bool
	go func() {
		defer close(beatDone)
		d.heartbeat(claim.Job.ID, &downloaded, &total, stopBeat, &lost, cancel)
	}()

	result, err := d.library.Install(ctx, claim.Request, func(done, size int64) {
		downloaded.Store(done)
		if size > 0 {
			total.Store(size)
		}
	})
	close(stopBeat)
	<-beatDone

	if lost.Load() {
		return
	}

	persist, stop := context.WithTimeout(context.WithoutCancel(d.lifetime), persistTimeout)
	defer stop()
	if err == nil {
		if beatErr := d.queue.Heartbeat(persist, claim.Job.ID, downloaded.Load(), total.Load(), d.Lease); beatErr != nil {
			if errors.Is(beatErr, rom.ErrLeaseLost) {
				d.logger.Warn("download lease lost", "job", claim.Job.ID, "error", beatErr)
				return
			}
			d.logger.Warn("record final download progress", "job", claim.Job.ID, "error", beatErr)
		}
	}
	d.finish(persist, claim, result, err)
}

func (d *Downloads) finish(ctx context.Context, claim rom.JobClaim, result rom.InstallResult, err error) {
	id := claim.Job.ID
	var persistErr error
	switch {
	case err == nil:
		persistErr = d.queue.Complete(ctx, id, result)
	case d.lifetime.Err() != nil:
		persistErr = d.queue.Release(ctx, id)
	case errors.Is(err, rom.ErrNotRetryable):
		d.logger.Error("download failed permanently", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.queue.Fail(ctx, id, err)
	case claim.Attempt < d.MaxAttempts:
		d.logger.Warn("download failed, retrying", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.queue.Retry(ctx, id, err, d.Now().Add(d.Backoff(claim.Attempt)))
	default:
		d.logger.Error("download failed", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.queue.Fail(ctx, id, err)
	}
	if persistErr != nil {
		if errors.Is(persistErr, rom.ErrLeaseLost) {
			d.logger.Warn("download lease lost", "job", id, "error", persistErr)
			return
		}
		d.logger.Error("record download outcome", "job", id, "error", persistErr)
	}
}

func (d *Downloads) heartbeat(id string, downloaded, total *atomic.Int64, stop <-chan struct{}, lost *atomic.Bool, cancel context.CancelFunc) {
	ticker := time.NewTicker(d.ProgressEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, stopBeat := context.WithTimeout(context.WithoutCancel(d.lifetime), persistTimeout)
			err := d.queue.Heartbeat(ctx, id, downloaded.Load(), total.Load(), d.Lease)
			stopBeat()
			if err == nil {
				continue
			}
			if errors.Is(err, rom.ErrLeaseLost) {
				lost.Store(true)
				d.logger.Warn("download lease lost", "job", id, "error", err)
				cancel()
				return
			}
			d.logger.Warn("record download progress", "job", id, "error", err)
		}
	}
}
