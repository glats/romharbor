package romdownloads

import (
	"context"
	"sync"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

// Enqueuer queues the download jobs for the download worker. It holds no
// library, no worker and no goroutine: it never runs a download, it only
// reports the queued job to the caller.
type Enqueuer struct {
	producer adapter.RomJobProducer

	mu     sync.Mutex
	closed bool
}

var _ adapter.RomDownloads = (*Enqueuer)(nil)

// NewEnqueuer builds the download queue of the web server.
func NewEnqueuer(producer adapter.RomJobProducer) *Enqueuer {
	return &Enqueuer{producer: producer}
}

// Start queues the download and reports the queued job.
func (e *Enqueuer) Start(ctx context.Context, req rom.InstallRequest) (rom.Job, error) {
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed {
		return rom.Job{}, rom.ErrShuttingDown
	}

	return e.producer.Enqueue(ctx, req)
}

// Shutdown stops the queue: the downloads started after it are refused.
func (e *Enqueuer) Shutdown(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true

	return nil
}
