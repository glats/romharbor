package romfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const (
	userAgent       = "ROMHarbor"
	maxRedirects    = 5
	defaultIdleTime = 60 * time.Second
)

var (
	ErrUnsupportedScheme   = errors.New("unsupported URL scheme")
	ErrMissingHost         = errors.New("URL has no host")
	ErrTooManyRedirects    = errors.New("too many redirects")
	ErrRangeNotSatisfiable = errors.New("requested range not satisfiable")
	ErrIdleTimeout         = errors.New("no data received within the idle timeout")
)

// URLError reports a download URL that was rejected before any network call.
type URLError struct {
	Err error
}

func (e *URLError) Error() string { return e.Err.Error() }

func (e *URLError) Unwrap() error { return e.Err }

// StatusError reports a response whose status the download cannot use.
type StatusError struct {
	Code   int
	Status string
}

func (e *StatusError) Error() string { return "unexpected HTTP status " + e.Status }

// Doer is the part of *http.Client the fetcher needs.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Stream is an opened download body.
type Stream struct {
	Body io.ReadCloser
	// Partial is true when the server honored the requested offset; false means
	// Body carries the whole file from its first byte.
	Partial bool
	// Length is the number of bytes Body will carry, or -1 when unknown.
	Length int64
	// ContentRange is the raw Content-Range response header, empty when absent.
	ContentRange string
	// Validator is the response ETag when it is a strong one, else its
	// Last-Modified, else empty. Pass it to WithIfRange to resume the same
	// representation later.
	Validator string
}

// OpenOption adjusts a single Open call.
type OpenOption func(*openConfig)

type openConfig struct {
	ifRange string
}

// WithIfRange makes a ranged request conditional on validator, so the server
// answers the whole file when the file changed. An empty validator adds no
// header.
func WithIfRange(validator string) OpenOption {
	return func(c *openConfig) { c.ifRange = validator }
}

// Fetcher opens download streams through a Doer under a fixed policy: only
// http and https URLs, at most five redirects that stay on http and https, and
// a bounded wait for the response and for every body read.
type Fetcher struct {
	doer Doer
	idle time.Duration
}

// New returns a Fetcher over doer; a nil doer uses NewClient(0). A *http.Client
// is copied so the redirect policy applies without touching the caller's client.
func New(doer Doer) *Fetcher {
	if doer == nil {
		doer = NewClient(0)
	}
	if client, ok := doer.(*http.Client); ok && client != nil {
		limited := *client
		limited.CheckRedirect = checkRedirect
		doer = &limited
	}
	return &Fetcher{doer: doer, idle: defaultIdleTime}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return ErrTooManyRedirects
	}
	if !httpScheme(req.URL) {
		return &URLError{Err: fmt.Errorf("%w %q", ErrUnsupportedScheme, req.URL.Scheme)}
	}
	return nil
}

func httpScheme(u *url.URL) bool { return u.Scheme == "http" || u.Scheme == "https" }

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, &URLError{Err: err}
	}
	if !httpScheme(u) {
		return nil, &URLError{Err: fmt.Errorf("%w %q", ErrUnsupportedScheme, u.Scheme)}
	}
	if u.Host == "" {
		return nil, &URLError{Err: ErrMissingHost}
	}
	return u, nil
}

// Open requests rawURL from offset onward. A positive offset sends a Range
// header; Stream.Partial tells whether the server honored it. The caller must
// close Stream.Body.
func (f *Fetcher) Open(ctx context.Context, rawURL string, offset int64, opts ...OpenOption) (*Stream, error) {
	var cfg openConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	u, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	idle := newIdleTimer(f.idle, cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		idle.stop()
		cancel()
		return nil, &URLError{Err: err}
	}
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if cfg.ifRange != "" {
			req.Header.Set("If-Range", cfg.ifRange)
		}
	}
	resp, err := f.doer.Do(req)
	idle.stop()
	if err != nil {
		cancel()
		if idle.fired() {
			return nil, fmt.Errorf("%w: %w", ErrIdleTimeout, err)
		}
		return nil, err
	}
	fail := func(err error) (*Stream, error) {
		_ = resp.Body.Close()
		cancel()
		return nil, err
	}
	if resp.Request != nil && resp.Request.URL != nil && !httpScheme(resp.Request.URL) {
		return fail(&URLError{Err: fmt.Errorf("%w %q", ErrUnsupportedScheme, resp.Request.URL.Scheme)})
	}
	partial := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if !partial && resp.StatusCode != http.StatusOK {
		if offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			return fail(ErrRangeNotSatisfiable)
		}
		return fail(&StatusError{Code: resp.StatusCode, Status: resp.Status})
	}
	return &Stream{
		Body:         &idleBody{body: resp.Body, timer: idle, cancel: cancel},
		Partial:      partial,
		Length:       resp.ContentLength,
		ContentRange: resp.Header.Get("Content-Range"),
		Validator:    validator(resp.Header),
	}, nil
}

func validator(h http.Header) string {
	if etag := h.Get("ETag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		return etag
	}
	return h.Get("Last-Modified")
}

type idleTimer struct {
	timer    *time.Timer
	d        time.Duration
	timedOut atomic.Bool
}

func newIdleTimer(d time.Duration, cancel context.CancelFunc) *idleTimer {
	t := &idleTimer{d: d}
	t.timer = time.AfterFunc(d, func() {
		t.timedOut.Store(true)
		cancel()
	})
	return t
}

func (t *idleTimer) arm()        { t.timer.Reset(t.d) }
func (t *idleTimer) stop()       { t.timer.Stop() }
func (t *idleTimer) fired() bool { return t.timedOut.Load() }

type idleBody struct {
	body   io.ReadCloser
	timer  *idleTimer
	cancel context.CancelFunc
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.timer.arm()
	n, err := b.body.Read(p)
	b.timer.stop()
	if err != nil && !errors.Is(err, io.EOF) && b.timer.fired() {
		return n, fmt.Errorf("%w: %w", ErrIdleTimeout, err)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.stop()
	err := b.body.Close()
	b.cancel()
	return err
}
