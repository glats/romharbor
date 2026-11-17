package romfetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func readAll(t *testing.T, s *Stream) string {
	t.Helper()
	body, err := io.ReadAll(s.Body)
	require.NoError(t, err)
	require.NoError(t, s.Body.Close())
	return string(body)
}

func TestOpenWholeFile(t *testing.T) {
	var gotRange, gotAgent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange.Store(r.Header.Get("Range"))
		gotAgent.Store(r.Header.Get("User-Agent"))
		_, _ = w.Write([]byte("0123456789"))
	}))
	t.Cleanup(srv.Close)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL, 0)

	require.NoError(t, err)
	assert.False(t, stream.Partial)
	assert.Equal(t, int64(10), stream.Length)
	assert.Empty(t, stream.ContentRange)
	assert.Equal(t, "0123456789", readAll(t, stream))
	assert.Empty(t, gotRange.Load(), "no Range header without an offset")
	assert.Equal(t, "ROMHarbor", gotAgent.Load())
}

func TestOpenResumesWithRange(t *testing.T) {
	var gotRange atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange.Store(r.Header.Get("Range"))
		w.Header().Set("Content-Range", "bytes 4-9/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("456789"))
	}))
	t.Cleanup(srv.Close)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL, 4)

	require.NoError(t, err)
	assert.True(t, stream.Partial)
	assert.Equal(t, int64(6), stream.Length)
	assert.Equal(t, "bytes 4-9/10", stream.ContentRange)
	assert.Equal(t, "456789", readAll(t, stream))
	assert.Equal(t, "bytes=4-", gotRange.Load())
}

func TestOpenReportsAServerThatIgnoresTheRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	t.Cleanup(srv.Close)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL, 4)

	require.NoError(t, err)
	assert.False(t, stream.Partial, "a 200 on a ranged request carries the whole file")
	assert.Equal(t, "0123456789", readAll(t, stream))
}

func TestOpenInterpretsStatuses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		offset     int64
		wantRange  bool
		wantStatus int
	}{
		{name: "unsatisfiable range on a resume", status: http.StatusRequestedRangeNotSatisfiable, offset: 5, wantRange: true},
		{name: "unsatisfiable range without a resume is a plain status", status: http.StatusRequestedRangeNotSatisfiable, wantStatus: 416},
		{name: "partial content without a resume is unexpected", status: http.StatusPartialContent, wantStatus: 206},
		{name: "server error", status: http.StatusInternalServerError, wantStatus: 500},
		{name: "not found on a resume", status: http.StatusNotFound, offset: 5, wantStatus: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var closed atomic.Bool
			f := New(doerFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tt.status,
					Status:     strconv.Itoa(tt.status) + " " + http.StatusText(tt.status),
					Header:     http.Header{},
					Body:       closeTracker{Reader: strings.NewReader("x"), closed: &closed},
					Request:    r,
				}, nil
			}))

			stream, err := f.Open(context.Background(), "http://example.test/game", tt.offset)

			require.Error(t, err)
			assert.Nil(t, stream)
			assert.True(t, closed.Load(), "the body must be closed on a rejected status")
			if tt.wantRange {
				assert.ErrorIs(t, err, ErrRangeNotSatisfiable)
				return
			}
			assert.NotErrorIs(t, err, ErrRangeNotSatisfiable)
			var statusErr *StatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, tt.wantStatus, statusErr.Code)
			assert.Equal(t, "unexpected HTTP status "+strconv.Itoa(tt.wantStatus)+" "+http.StatusText(tt.wantStatus), err.Error())
		})
	}
}

type closeTracker struct {
	io.Reader
	closed *atomic.Bool
}

func (c closeTracker) Close() error { c.closed.Store(true); return nil }

func TestOpenRejectsUnusableURLsBeforeTheNetwork(t *testing.T) {
	for _, target := range []string{
		"file:///etc/passwd",
		"ftp://example.test/game",
		"gopher://example.test/game",
		"//example.test/game",
		"game.sfc",
		"http:///game",
		"https://",
		"://bad",
	} {
		t.Run(target, func(t *testing.T) {
			var calls atomic.Int32
			f := New(doerFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("must not be reached")
			}))

			stream, err := f.Open(context.Background(), target, 0)

			assert.Nil(t, stream)
			var urlErr *URLError
			require.ErrorAs(t, err, &urlErr)
			assert.Zero(t, calls.Load())
		})
	}
}

func TestOpenNamesTheUnsupportedScheme(t *testing.T) {
	_, err := New(nil).Open(context.Background(), "ftp://example.test/game", 0)

	assert.ErrorIs(t, err, ErrUnsupportedScheme)
	assert.ErrorContains(t, err, "ftp")
}

func TestOpenAcceptsHTTPAndHTTPSSchemesInAnyCase(t *testing.T) {
	var seen atomic.Value
	f := New(doerFunc(func(r *http.Request) (*http.Response, error) {
		seen.Store(r.URL.Scheme)
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))

	for _, target := range []string{"HTTP://example.test/a", "https://example.test/a"} {
		stream, err := f.Open(context.Background(), target, 0)
		require.NoError(t, err, target)
		require.NoError(t, stream.Body.Close())
	}
}

func TestOpenFollowsUpToFiveRedirects(t *testing.T) {
	srv := redirectChain(t, 5)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL+"/hop/0", 0)

	require.NoError(t, err)
	assert.Equal(t, "arrived", readAll(t, stream))
}

func TestOpenStopsAfterFiveRedirects(t *testing.T) {
	srv := redirectChain(t, 6)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL+"/hop/0", 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrTooManyRedirects)
}

func TestOpenAppliesTheRedirectPolicyToTheDefaultClient(t *testing.T) {
	srv := redirectChain(t, 6)
	f := New(nil)
	require.NotNil(t, f)

	stream, err := f.Open(context.Background(), srv.URL+"/hop/0", 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrTooManyRedirects)
}

func TestOpenRefusesARedirectToAnotherScheme(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	stream, err := New(srv.Client()).Open(context.Background(), srv.URL, 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrUnsupportedScheme)
	assert.EqualValues(t, 1, hits.Load())
}

func redirectChain(t *testing.T, hops int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
		if err != nil || n >= hops {
			_, _ = w.Write([]byte("arrived"))
			return
		}
		http.Redirect(w, r, "/hop/"+strconv.Itoa(n+1), http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBodyFailsWhenNoBytesArriveWithinTheIdleTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("01234"))
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	f := New(srv.Client())
	f.idle = 100 * time.Millisecond

	stream, err := f.Open(context.Background(), srv.URL, 0)
	require.NoError(t, err)
	defer func() { _ = stream.Body.Close() }()

	start := time.Now()
	body, err := io.ReadAll(stream.Body)

	assert.ErrorIs(t, err, ErrIdleTimeout)
	assert.Equal(t, "01234", string(body))
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestOpenFailsWhenTheServerNeverAnswers(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	f := New(srv.Client())
	f.idle = 100 * time.Millisecond

	stream, err := f.Open(context.Background(), srv.URL, 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrIdleTimeout)
}

func TestBodyKeepsReadingWhileBytesKeepArriving(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 5 {
			_, _ = w.Write([]byte("ab"))
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)
	f := New(srv.Client())
	f.idle = 250 * time.Millisecond

	stream, err := f.Open(context.Background(), srv.URL, 0)
	require.NoError(t, err)

	assert.Equal(t, "ababababab", readAll(t, stream))
}

func TestContextCancellationReachesTheBody(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("01234"))
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	ctx, cancel := context.WithCancel(context.Background())

	stream, err := New(srv.Client()).Open(ctx, srv.URL, 0)
	require.NoError(t, err)
	defer func() { _ = stream.Body.Close() }()
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err = io.ReadAll(stream.Body)

	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, ErrIdleTimeout)
}

func TestOpenPropagatesTransportErrors(t *testing.T) {
	boom := errors.New("connection refused")
	f := New(doerFunc(func(*http.Request) (*http.Response, error) { return nil, boom }))

	stream, err := f.Open(context.Background(), "http://example.test/game", 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, boom)
}

func TestOpenHonorsACanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)

	stream, err := New(srv.Client()).Open(ctx, srv.URL, 0)

	assert.Nil(t, stream)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewDoesNotMutateTheCallersClient(t *testing.T) {
	client := &http.Client{}

	New(client)

	assert.Nil(t, client.CheckRedirect)
}
