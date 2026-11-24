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

// Runner claims the queued downloads and runs them: it recovers the abandoned
// ones, keeps the lease of the job it holds, records the progress and retries
// or fails the job when the download does not succeed. It is the download
// worker; the web server only queues the jobs.
type Runner struct {
	policy      Policy
	library     adapter.RomLibrary
	worker      adapter.RomJobWorker
	maintenance adapter.RomJobMaintenance
	logger      *slog.Logger
	now         func() time.Time

	lifetime context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup

	mu      sync.Mutex
	resumed bool
}

// Option changes how the download runner is built.
type Option func(*Runner)

// WithClock sets the clock the runner measures the retention with.
func WithClock(now func() time.Time) Option {
	return func(r *Runner) { r.now = now }
}

// WithLogger sets the logger the runner reports with.
func WithLogger(logger *slog.Logger) Option {
	return func(r *Runner) {
		if logger != nil {
			r.logger = logger
		}
	}
}

// NewRunner builds the download runner.
func NewRunner(library adapter.RomLibrary, jobs JobPorts, policy Policy, options ...Option) *Runner {
	lifetime, cancel := context.WithCancel(context.Background())
	r := &Runner{
		policy:      policy,
		library:     library,
		worker:      jobs.Worker,
		maintenance: jobs.Maintenance,
		logger:      slog.Default(),
		now:         time.Now,
		lifetime:    lifetime,
		cancel:      cancel,
	}
	for _, option := range options {
		option(r)
	}
	return r
}

func defaultBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}

func (r *Runner) Resume(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lifetime.Err() != nil {
		return rom.ErrShuttingDown
	}
	if r.resumed {
		return nil
	}
	recovered, err := r.maintenance.Recover(ctx)
	if err != nil {
		return fmt.Errorf("recover interrupted downloads: %w", err)
	}
	if recovered > 0 {
		r.logger.Info("interrupted downloads queued again", "jobs", recovered)
	}
	if _, err := r.maintenance.Prune(ctx, r.now().Add(-r.policy.Retention)); err != nil {
		return fmt.Errorf("prune old downloads: %w", err)
	}
	r.resumed = true
	for range max(r.policy.Workers, 1) {
		r.workers.Add(1)
		go r.work()
	}
	return nil
}

func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.cancel()
	r.mu.Unlock()
	done := make(chan struct{})
	go func() {
		r.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for downloads: %w", ctx.Err())
	}
}

func (r *Runner) work() {
	defer r.workers.Done()
	for r.lifetime.Err() == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.lifetime), persistTimeout)
		claim, ok, err := r.worker.Claim(ctx, r.policy.Lease)
		cancel()
		if err != nil {
			r.logger.Error("claim download", "error", err)
			r.idle()
			continue
		}
		if !ok {
			r.recover()
			r.idle()
			continue
		}
		r.run(claim)
	}
}

func (r *Runner) recover() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.lifetime), persistTimeout)
	defer cancel()
	recovered, err := r.maintenance.Recover(ctx)
	if err != nil {
		r.logger.Error("recover downloads", "error", err)
		return
	}
	if recovered > 0 {
		r.logger.Info("downloads with an expired lease queued again", "jobs", recovered)
	}
}

func (r *Runner) idle() {
	timer := time.NewTimer(r.policy.Poll)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.lifetime.Done():
	}
}

func (r *Runner) run(claim adapter.RomJobClaim) {
	ctx, cancel := context.WithTimeout(r.lifetime, r.policy.Timeout)
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
		r.heartbeat(lease, &downloaded, &total, stopBeat, &lost, cancel)
	}()

	result, err := r.library.Install(ctx, claim.Request, func(done, size int64) {
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

	persist, stop := context.WithTimeout(context.WithoutCancel(r.lifetime), persistTimeout)
	defer stop()
	if err == nil {
		renewed, beatErr := r.worker.Heartbeat(persist, lease.get(), downloaded.Load(), total.Load(), r.policy.Lease)
		if beatErr != nil {
			if errors.Is(beatErr, rom.ErrLeaseLost) {
				r.logger.Warn("download lease lost", "job", claim.Job.ID, "error", beatErr)
				return
			}
			r.logger.Warn("record final download progress", "job", claim.Job.ID, "error", beatErr)
		} else {
			lease.set(renewed)
		}
	}
	r.finish(persist, claim, lease.get(), result, err)
}

func (r *Runner) finish(ctx context.Context, claim adapter.RomJobClaim, lease adapter.RomJobLease, result rom.InstallResult, err error) {
	id := claim.Job.ID
	var persistErr error
	switch {
	case err == nil:
		persistErr = r.worker.Complete(ctx, lease, result)
	case r.lifetime.Err() != nil:
		persistErr = r.worker.Release(ctx, lease)
	case errors.Is(err, rom.ErrNotRetryable):
		r.logger.Error("download failed permanently", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = r.worker.Fail(ctx, lease, err)
	case claim.Attempt < r.policy.MaxAttempts:
		r.logger.Warn("download failed, retrying", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = r.worker.Retry(ctx, lease, err, r.now().Add(r.policy.Backoff(claim.Attempt)))
	default:
		r.logger.Error("download failed", "game", claim.Job.Title, "attempt", claim.Attempt, "error", err)
		persistErr = r.worker.Fail(ctx, lease, err)
	}
	if persistErr != nil {
		if errors.Is(persistErr, rom.ErrLeaseLost) {
			r.logger.Warn("download lease lost", "job", id, "error", persistErr)
			return
		}
		r.logger.Error("record download outcome", "job", id, "error", persistErr)
	}
}

func (r *Runner) heartbeat(lease *heldLease, downloaded, total *atomic.Int64, stop <-chan struct{}, lost *atomic.Bool, cancel context.CancelFunc) {
	ticker := time.NewTicker(r.policy.ProgressEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, stopBeat := context.WithTimeout(context.WithoutCancel(r.lifetime), persistTimeout)
			renewed, err := r.worker.Heartbeat(ctx, lease.get(), downloaded.Load(), total.Load(), r.policy.Lease)
			stopBeat()
			if err == nil {
				lease.set(renewed)
				continue
			}
			if errors.Is(err, rom.ErrLeaseLost) {
				lost.Store(true)
				r.logger.Warn("download lease lost", "job", lease.get().JobID, "error", err)
				cancel()
				return
			}
			r.logger.Warn("record download progress", "job", lease.get().JobID, "error", err)
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
