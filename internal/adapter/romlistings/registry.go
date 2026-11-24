package romlistings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

// DefaultSourceTimeout is the time one source may take to answer a query.
const DefaultSourceTimeout = 20 * time.Second

type registry struct {
	sources []adapter.RomSource
	timeout time.Duration
	logger  *slog.Logger
}

func newRegistry(sources []adapter.RomSource, timeout time.Duration, logger *slog.Logger) *registry {
	if timeout <= 0 {
		timeout = DefaultSourceTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &registry{sources: sources, timeout: timeout, logger: logger}
}

type sourceResult struct {
	name  string
	games []rom.Game
	err   error
}

// search queries every source that serves the query in parallel, each one
// under its own timeout, and merges the answers in configuration order: the
// first configured source wins a duplicated game ID.
func (r *registry) search(ctx context.Context, q rom.Query) (rom.GamePage, error) {
	queried := r.queried(q)
	results := make([]sourceResult, len(queried))
	var wg sync.WaitGroup
	for index, source := range queried {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sourceCtx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()
			games, err := source.Search(sourceCtx, q)
			results[index] = sourceResult{name: source.Name(), games: games, err: err}
		}()
	}
	wg.Wait()

	page := rom.GamePage{}
	var lists [][]rom.Game
	var errs []error
	for _, result := range results {
		if result.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", result.name, result.err))
			page.FailedSources = append(page.FailedSources, result.name)
			r.logger.Error("source search failed", "source", result.name, "error", result.err)
			continue
		}
		lists = append(lists, result.games)
	}
	page.Games = rom.MergeGames(lists...)
	if len(queried) > 0 && len(errs) == len(queried) {
		return page, errs[0]
	}
	return page, nil
}

// queried returns the sources that serve the query, in configuration order.
func (r *registry) queried(q rom.Query) []adapter.RomSource {
	var out []adapter.RomSource
	for _, s := range r.sources {
		if !serves(s, q.Platform) {
			continue
		}
		searchOnly, ok := s.(adapter.RomSearchOnlySource)
		if (ok && searchOnly.SearchOnly()) != (q.Text != "") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func serves(s adapter.RomSource, platform string) bool {
	plats := s.Platforms()
	if len(plats) == 0 {
		return true
	}
	for _, p := range plats {
		if p == platform {
			return true
		}
	}
	return false
}
