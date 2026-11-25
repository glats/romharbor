package romcovers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), []byte("image-body")...)

func upstream(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Option) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, func(s *Store) {
		WithDoer(server.Client())(s)
		WithURLRewrite(func(string) string { return server.URL + "/cover.png" })(s)
	}
}

func TestCoverIsFetchedOnceAndThenServedFromDisk(t *testing.T) {
	var requests atomic.Int32
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(pngBytes)
	})
	dir := t.TempDir()
	store := New(dir, rewrite)

	first, err := store.Cover(context.Background(), "n64", "Killer Instinct Gold (USA)")
	require.NoError(t, err)
	second, err := New(dir, rewrite).Cover(context.Background(), "n64", "Killer Instinct Gold (USA)")
	require.NoError(t, err)

	assert.Equal(t, pngBytes, first.Data)
	assert.Equal(t, "image/png", first.ContentType)
	assert.Equal(t, pngBytes, second.Data)
	assert.EqualValues(t, 1, requests.Load())
}

func TestMissingCoverIsRememberedForADay(t *testing.T) {
	var requests atomic.Int32
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.NotFound(w, nil)
	})
	now := time.Now()
	store := New(t.TempDir(), rewrite, WithClock(func() time.Time { return now }))

	for range 3 {
		_, err := store.Cover(context.Background(), "n64", "Nope")
		assert.ErrorIs(t, err, rom.ErrCoverNotFound)
	}
	assert.EqualValues(t, 1, requests.Load())

	now = now.Add(25 * time.Hour)
	_, err := store.Cover(context.Background(), "n64", "Nope")
	assert.ErrorIs(t, err, rom.ErrCoverNotFound)
	assert.EqualValues(t, 2, requests.Load())
}

func TestUpstreamFailureIsRememberedForTwoMinutesOnly(t *testing.T) {
	var requests atomic.Int32
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	})
	now := time.Now()
	store := New(t.TempDir(), rewrite, WithClock(func() time.Time { return now }))

	for range 2 {
		_, err := store.Cover(context.Background(), "n64", "Slow")
		assert.ErrorIs(t, err, ErrUnavailable)
	}
	assert.EqualValues(t, 1, requests.Load())

	now = now.Add(3 * time.Minute)
	_, err := store.Cover(context.Background(), "n64", "Slow")
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.EqualValues(t, 2, requests.Load())
}

func TestConcurrentRequestsShareOneFetch(t *testing.T) {
	var requests atomic.Int32
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write(pngBytes)
	})
	store := New(t.TempDir(), rewrite)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cover, err := store.Cover(context.Background(), "snes", "Shared")
			assert.NoError(t, err)
			assert.Equal(t, pngBytes, cover.Data)
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, requests.Load())
}

func TestAtMostFourFetchesRunAtOnce(t *testing.T) {
	var running, peak atomic.Int32
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		now := running.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
		running.Add(-1)
		_, _ = w.Write(pngBytes)
	})
	store := New(t.TempDir(), rewrite)

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Cover(context.Background(), "snes", "Title "+string(rune('A'+i)))
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.LessOrEqual(t, peak.Load(), int32(upstreamSlots))
}

func TestACallerThatGivesUpDoesNotCancelTheSharedFetch(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write(pngBytes)
	})
	dir := t.TempDir()
	store := New(dir, rewrite)

	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := store.Cover(short, "snes", "Patient")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	require.Eventually(t, func() bool {
		cover, err := store.Cover(context.Background(), "snes", "Patient")
		return err == nil && string(cover.Data) == string(pngBytes)
	}, 3*time.Second, 50*time.Millisecond)
}

func TestARemoteThatIsNotAPNGIsTreatedAsMissing(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not an image</html>"))
	})
	store := New(t.TempDir(), rewrite)

	_, err := store.Cover(context.Background(), "snes", "Html")

	assert.ErrorIs(t, err, rom.ErrCoverNotFound)
}

func TestAnOversizedImageIsNotStored(t *testing.T) {
	big := append(append([]byte{}, pngBytes...), make([]byte, maxImageBytes)...)
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(big) })
	dir := t.TempDir()
	store := New(dir, rewrite)

	_, err := store.Cover(context.Background(), "snes", "Huge")

	assert.ErrorIs(t, err, rom.ErrCoverNotFound)
	files, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	assert.Empty(t, files)
}

func TestUnknownPlatformAndEmptyTitleNeverReachTheNetwork(t *testing.T) {
	var requests atomic.Int32
	_, rewrite := upstream(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
	store := New(t.TempDir(), rewrite)

	_, unknown := store.Cover(context.Background(), "nope", "Game")
	_, empty := store.Cover(context.Background(), "snes", "")

	assert.ErrorIs(t, unknown, rom.ErrUnknownPlatform)
	assert.ErrorIs(t, empty, rom.ErrCoverNotFound)
	assert.Zero(t, requests.Load())
}

func TestTheCachedFileIsReadableByOtherUsers(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pngBytes) })
	dir := t.TempDir()
	store := New(dir, rewrite)

	_, err := store.Cover(context.Background(), "snes", "Perm")
	require.NoError(t, err)

	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	info, err := os.Stat(files[0])
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func sizedPNG(label string, size int) []byte {
	return append(append([]byte{}, pngBytes...), []byte(strings.Repeat(label, size))...)
}

func TestTheCacheStaysUnderItsSizeCapByEvictingTheLeastRecentlyUsed(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sizedPNG("x", 400))
	})
	dir := t.TempDir()
	store := New(dir, rewrite, WithMaxBytes(1500))
	base := time.Now().Add(-time.Hour)

	titles := []string{"A", "B", "C", "D", "E"}
	for i, title := range titles {
		_, err := store.Cover(context.Background(), "snes", title)
		require.NoError(t, err)
		require.NoError(t, os.Chtimes(filepath.Join(dir, cacheKey("snes", title)+".png"), base.Add(time.Duration(i)*time.Minute), base.Add(time.Duration(i)*time.Minute)))
	}
	_, err := store.Cover(context.Background(), "snes", "F")
	require.NoError(t, err)

	var total int64
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	require.NoError(t, err)
	for _, file := range files {
		info, err := os.Stat(file)
		require.NoError(t, err)
		total += info.Size()
	}
	assert.LessOrEqual(t, total, int64(1500))
	assert.NoFileExists(t, filepath.Join(dir, cacheKey("snes", "A")+".png"))
	assert.FileExists(t, filepath.Join(dir, cacheKey("snes", "F")+".png"))
}

func TestACachedCoverThatIsReadAgainSurvivesEviction(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sizedPNG("x", 300))
	})
	dir := t.TempDir()
	later := time.Now().Add(time.Hour)
	store := New(dir, rewrite, WithMaxBytes(1000), WithClock(func() time.Time { return later }))
	old := time.Now().Add(-time.Hour)

	for i, title := range []string{"A", "B"} {
		_, err := store.Cover(context.Background(), "snes", title)
		require.NoError(t, err)
		stamp := old.Add(time.Duration(i) * time.Minute)
		require.NoError(t, os.Chtimes(filepath.Join(dir, cacheKey("snes", title)+".png"), stamp, stamp))
	}
	_, err := store.Cover(context.Background(), "snes", "A")
	require.NoError(t, err)
	for _, title := range []string{"C", "D"} {
		_, err := store.Cover(context.Background(), "snes", title)
		require.NoError(t, err)
	}

	assert.FileExists(t, filepath.Join(dir, cacheKey("snes", "A")+".png"))
	assert.NoFileExists(t, filepath.Join(dir, cacheKey("snes", "B")+".png"))
}

func TestExpiredMissingMarkersAreSweptWhenTheCachePrunes(t *testing.T) {
	_, rewrite := upstream(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(sizedPNG("x", 400))
	})
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.miss")
	require.NoError(t, os.WriteFile(stale, []byte(markerMissing), 0o644))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))
	store := New(dir, rewrite, WithMaxBytes(300))

	_, err := store.Cover(context.Background(), "snes", "A")
	require.NoError(t, err)

	assert.NoFileExists(t, stale)
}
