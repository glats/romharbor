package romcovers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/domain/rom"
)

const (
	maxImageBytes     = 2 << 20
	fetchTimeout      = 15 * time.Second
	missingTTL        = 24 * time.Hour
	unavailableTTL    = 2 * time.Minute
	upstreamSlots     = 4
	defaultMaxBytes   = 256 << 20
	pruneTarget       = 80
	notFoundStatus    = 404
	markerMissing     = "missing"
	markerUnavailable = "unavailable"
)

var ErrUnavailable = errors.New("cover source unavailable")

type Store struct {
	dir     string
	fetcher *romfetch.Fetcher
	rewrite func(string) string
	now     func() time.Time
	slots   chan struct{}

	maxBytes int64

	mu       sync.Mutex
	inflight map[string]*fetch

	pruneMu sync.Mutex
	sized   bool
	bytes   int64
}

type fetch struct {
	done  chan struct{}
	cover rom.Cover
	err   error
}

type Option func(*Store)

func WithDoer(doer romfetch.Doer) Option {
	return func(s *Store) { s.fetcher = romfetch.New(doer) }
}

func WithURLRewrite(rewrite func(string) string) Option {
	return func(s *Store) { s.rewrite = rewrite }
}

func WithMaxBytes(limit int64) Option {
	return func(s *Store) { s.maxBytes = limit }
}

func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

func New(dir string, options ...Option) *Store {
	s := &Store{
		dir:      dir,
		fetcher:  romfetch.New(romfetch.NewClient(0)),
		now:      time.Now,
		maxBytes: defaultMaxBytes,
		slots:    make(chan struct{}, upstreamSlots),
		inflight: map[string]*fetch{},
	}
	for _, option := range options {
		option(s)
	}
	return s
}

func (s *Store) Cover(ctx context.Context, platformSlug, title string) (rom.Cover, error) {
	platform, ok := rom.PlatformBySlug(platformSlug)
	if !ok {
		return rom.Cover{}, rom.ErrUnknownPlatform
	}
	upstream := platform.CoverURL(title)
	if upstream == "" {
		return rom.Cover{}, rom.ErrCoverNotFound
	}
	if s.rewrite != nil {
		upstream = s.rewrite(upstream)
	}
	key := cacheKey(platform.Slug, title)
	if cover, found := s.stored(key); found {
		return cover, nil
	}
	if err := s.remembered(key); err != nil {
		return rom.Cover{}, err
	}
	return s.share(ctx, key, upstream)
}

func cacheKey(platform, title string) string {
	sum := sha256.Sum256([]byte(platform + "\x00" + title))
	return hex.EncodeToString(sum[:])
}

func (s *Store) imagePath(key string) string  { return filepath.Join(s.dir, key+".png") }
func (s *Store) markerPath(key string) string { return filepath.Join(s.dir, key+".miss") }

func (s *Store) stored(key string) (rom.Cover, bool) {
	data, err := os.ReadFile(s.imagePath(key))
	if err != nil || !isPNG(data) {
		return rom.Cover{}, false
	}
	now := s.now()
	_ = os.Chtimes(s.imagePath(key), now, now)
	return rom.Cover{Data: data, ContentType: "image/png"}, true
}

func (s *Store) remembered(key string) error {
	path := s.markerPath(key)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	reason, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ttl, failure := missingTTL, rom.ErrCoverNotFound
	if strings.TrimSpace(string(reason)) == markerUnavailable {
		ttl, failure = unavailableTTL, ErrUnavailable
	}
	if s.now().Sub(info.ModTime()) >= ttl {
		_ = os.Remove(path)
		return nil
	}
	return failure
}

func (s *Store) share(ctx context.Context, key, upstream string) (rom.Cover, error) {
	s.mu.Lock()
	call, running := s.inflight[key]
	if !running {
		call = &fetch{done: make(chan struct{})}
		s.inflight[key] = call
		go s.run(key, upstream, call)
	}
	s.mu.Unlock()

	select {
	case <-call.done:
		return call.cover, call.err
	case <-ctx.Done():
		return rom.Cover{}, ctx.Err()
	}
}

func (s *Store) run(key, upstream string, call *fetch) {
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(call.done)
	}()
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	call.cover, call.err = s.download(ctx, key, upstream)
}

func (s *Store) download(ctx context.Context, key, upstream string) (rom.Cover, error) {
	stream, err := s.fetcher.Open(ctx, upstream, 0)
	if err != nil {
		var status *romfetch.StatusError
		if errors.As(err, &status) && status.Code == notFoundStatus {
			s.remember(key, markerMissing)
			return rom.Cover{}, rom.ErrCoverNotFound
		}
		s.remember(key, markerUnavailable)
		return rom.Cover{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = stream.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(stream.Body, maxImageBytes+1))
	if err != nil {
		s.remember(key, markerUnavailable)
		return rom.Cover{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(data) > maxImageBytes || !isPNG(data) {
		s.remember(key, markerMissing)
		return rom.Cover{}, rom.ErrCoverNotFound
	}
	if s.write(s.imagePath(key), data) == nil {
		s.grew(int64(len(data)))
	}
	return rom.Cover{Data: data, ContentType: "image/png"}, nil
}

func (s *Store) remember(key, reason string) {
	_ = s.write(s.markerPath(key), []byte(reason))
}

func (s *Store) write(path string, data []byte) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(s.dir, ".cover-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil && !errors.Is(err, fs.ErrExist) {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func isPNG(data []byte) bool {
	return len(data) > 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n"
}

func (s *Store) grew(added int64) {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	if s.sized {
		s.bytes += added
	} else {
		s.sized = true
		s.bytes = 0
		for _, entry := range s.entries() {
			s.bytes += entry.size
		}
	}
	if s.maxBytes > 0 && s.bytes > s.maxBytes {
		s.prune()
	}
}

type cached struct {
	path    string
	size    int64
	modTime time.Time
	marker  bool
}

func (s *Store) entries() []cached {
	listing, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	found := make([]cached, 0, len(listing))
	for _, item := range listing {
		name := item.Name()
		marker := strings.HasSuffix(name, ".miss")
		if item.IsDir() || (!marker && !strings.HasSuffix(name, ".png")) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		found = append(found, cached{path: filepath.Join(s.dir, name), size: info.Size(), modTime: info.ModTime(), marker: marker})
	}
	return found
}

func (s *Store) prune() {
	now := s.now()
	var total int64
	var kept []cached
	for _, entry := range s.entries() {
		if entry.marker && now.Sub(entry.modTime) >= missingTTL {
			_ = os.Remove(entry.path)
			continue
		}
		total += entry.size
		kept = append(kept, entry)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].modTime.Before(kept[j].modTime) })
	goal := s.maxBytes * pruneTarget / 100
	for _, entry := range kept {
		if total <= goal {
			break
		}
		if os.Remove(entry.path) == nil {
			total -= entry.size
		}
	}
	s.bytes = total
}
