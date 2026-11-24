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

const persistTimeout = 3 * time.Second

// JobPorts are the job queue ports the download runner uses.
type JobPorts struct {
	Producer    adapter.RomJobProducer
	Worker      adapter.RomJobWorker
	Maintenance adapter.RomJobMaintenance
}

type Downloads struct {
	policy      Policy
	library     adapter.RomLibrary
	producer    adapter.RomJobProducer
	worker      adapter.RomJobWorker
	maintenance adapter.RomJobMaintenance
	logger      *slog.Logger
	now         func() time.Time

	lifetime context.Context
	cancel   context.CancelFunc
	wake     chan struct{}
	workers  sync.WaitGroup

	mu      sync.Mutex
	resumed bool
}

var _ adapter.RomDownloads = (*Downloads)(nil)

// Option changes how the download runner is built.
type Option func(*Downloads)

// WithClock sets the clock the runner measures the retention with.
func WithClock(now func() time.Time) Option {
	return func(d *Downloads) { d.now = now }
}

// WithLogger sets the logger the runner reports with.
func WithLogger(logger *slog.Logger) Option {
	return func(d *Downloads) {
		if logger != nil {
			d.logger = logger
		}
	}
}

func NewDownloads(library adapter.RomLibrary, jobs JobPorts, policy Policy, options ...Option) *Downloads {
	lifetime, cancel := context.WithCancel(context.Background())
	d := &Downloads{
		policy:      policy,
		library:     library,
		producer:    jobs.Producer,
		worker:      jobs.Worker,
		maintenance: jobs.Maintenance,
		logger:      slog.Default(),
		now:         time.Now,
		lifetime:    lifetime,
		cancel:      cancel,
		wake:        make(chan struct{}, 1),
	}
	for _, option := range options {
		option(d)
	}
	return d
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
	recovered, err := d.maintenance.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover interrupted downloads: %w", err)
	}
	if recovered > 0 {
		d.logger.Info("interrupted downloads queued again", "jobs", recovered)
	}
	if _, err := d.maintenance.Prune(ctx, d.now().Add(-d.policy.Retention)); err != nil {
		return fmt.Errorf("prune old downloads: %w", err)
	}
	d.resumed = true
	for range max(d.policy.Workers, 1) {
		d.workers.Add(1)
		go d.work()
	}
	return nil
}

func (d *Downloads) Start(ctx context.Context, req rom.InstallRequest) (rom.Job, error) {
	if d.lifetime.Err() != nil {
		return rom.Job{}, rom.ErrShuttingDown
	}
	job, err := d.producer.Enqueue(ctx, req)
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
		claim, ok, err := d.worker.Claim(ctx, d.policy.Lease)
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
	recovered, err := d.maintenance.Recover(ctx)
	if err != nil {
		d.logger.Error("recover downloads", "error", err)
		return
	}
	if recovered > 0 {
		d.logger.Info("downloads with an expired lease queued again", "jobs", recovered)
	}
}

func (d *Downloads) idle() {
	timer := time.NewTimer(d.policy.Poll)
	defer timer.Stop()
	select {
	case <-d.wake:
	case <-timer.C:
	case <-d.lifetime.Done():
	}
}

func (d *Downloads) run(claim adapter.RomJobClaim) {
	ctx, cancel := context.WithTimeout(d.lifetime, d.policy.Timeout)
	defer cancel()

	var downloaded, total atomic.Int64
	downloaded.Store(claim.Job.Downloaded)
	total.Store(claim.Job.Total)
	lease := &heldLease{current: claim.Lease}
	stopBeat := make(chan struct{})
	beatDone := make(chan struct{})
	var lost atomic.Bool
	go func() {
		defer close(beatDone)
		d.heartbeat(lease, &downloaded, &total, stopBeat, &lost, cancel)
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
		renewed, beatErr := d.worker.Heartbeat(persist, lease.get(), downloaded.Load(), total.Load(), d.policy.Lease)
		if beatErr != nil {
			if errors.Is(beatErr, rom.ErrLeaseLost) {
				d.logger.Warn("download lease lost", "job", claim.Job.ID, "error", beatErr)
				return
			}
			d.logger.Warn("record final download progress", "job", claim.Job.ID, "error", beatErr)
		} else {
			lease.set(renewed)
		}
	}
	d.finish(persist, claim, lease.get(), result, err)
}

func (d *Downloads) finish(ctx context.Context, claim adapter.RomJobClaim, lease adapter.RomJobLease, result rom.InstallResult, err error) {
	id := claim.Job.ID
	var persistErr error
	switch {
	case err == nil:
		persistErr = d.worker.Complete(ctx, lease, result)
	case d.lifetime.Err() != nil:
		persistErr = d.worker.Release(ctx, lease)
	case errors.Is(err, rom.ErrNotRetryable):
		d.logger.Error("download failed permanently", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.worker.Fail(ctx, lease, err)
	case claim.Attempt < d.policy.MaxAttempts:
		d.logger.Warn("download failed, retrying", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.worker.Retry(ctx, lease, err, d.now().Add(d.policy.Backoff(claim.Attempt)))
	default:
		d.logger.Error("download failed", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = d.worker.Fail(ctx, lease, err)
	}
	if persistErr != nil {
		if errors.Is(persistErr, rom.ErrLeaseLost) {
			d.logger.Warn("download lease lost", "job", id, "error", persistErr)
			return
		}
		d.logger.Error("record download outcome", "job", id, "error", persistErr)
	}
}

func (d *Downloads) heartbeat(lease *heldLease, downloaded, total *atomic.Int64, stop <-chan struct{}, lost *atomic.Bool, cancel context.CancelFunc) {
	ticker := time.NewTicker(d.policy.ProgressEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, stopBeat := context.WithTimeout(context.WithoutCancel(d.lifetime), persistTimeout)
			renewed, err := d.worker.Heartbeat(ctx, lease.get(), downloaded.Load(), total.Load(), d.policy.Lease)
			stopBeat()
			if err == nil {
				lease.set(renewed)
				continue
			}
			if errors.Is(err, rom.ErrLeaseLost) {
				lost.Store(true)
				d.logger.Warn("download lease lost", "job", lease.get().JobID, "error", err)
				cancel()
				return
			}
			d.logger.Warn("record download progress", "job", lease.get().JobID, "error", err)
		}
	}
}

// heldLease is the lease a worker currently holds: every renewal replaces it.
type heldLease struct {
	mu      sync.Mutex
	current adapter.RomJobLease
}

func (l *heldLease) get() adapter.RomJobLease {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current
}

func (l *heldLease) set(lease adapter.RomJobLease) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.current = lease
}
