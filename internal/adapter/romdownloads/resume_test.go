package romdownloads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
)

func storedAttempts(t *testing.T, path, id string) (status string, attempts int, errText string) {
	t.Helper()
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	require.NoError(t, raw.QueryRow(`SELECT status, attempts, error FROM jobs WHERE id = ?`, id).Scan(&status, &attempts, &errText))
	return status, attempts, errText
}

func TestARestartResumesAPartialFileWithARangeRequest(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	sum := sha256.Sum256(data)
	file := rom.File{Name: "g1.sfc", Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	var (
		mu     sync.Mutex
		ranges []string
		phase  atomic.Int32
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		if phase.Load() == 0 {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:len(data)/3])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "g1", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(upstream.Close)
	file.URL = upstream.URL
	game := rom.Game{ID: "g1", Platform: "snes", Title: "G1", Files: []rom.File{file}}
	platform, _ := rom.PlatformBySlug("snes")
	request := rom.InstallRequest{Game: game, File: file, Platform: platform}

	library := &romlibrary.Library{Root: t.TempDir(), HTTPClient: upstream.Client()}
	store, path := openStore(t)
	first := newTestDownloads(t, &fakeLibrary{install: library.Install}, store)
	require.NoError(t, first.Resume(context.Background()))
	job, err := first.Start(context.Background(), request)
	require.NoError(t, err)
	waitForJob(t, store, job.ID, func(j rom.Job) bool { return j.Status == rom.JobRunning && j.Downloaded > 0 })
	require.NoError(t, first.Shutdown(context.Background()))

	state, attempts, errText := storedAttempts(t, path, job.ID)
	assert.Equal(t, rom.JobQueued, state)
	assert.Zero(t, attempts, "an interrupted download does not consume an attempt")
	assert.Empty(t, errText)

	phase.Store(1)
	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	reopened := sqlite.NewJobStore(db)
	second := newTestDownloads(t, &fakeLibrary{install: library.Install}, reopened)
	require.NoError(t, second.Resume(context.Background()))

	done := waitForJob(t, reopened, job.ID, status(rom.JobDone))
	installed, err := os.ReadFile(filepath.Join(library.Root, filepath.FromSlash(done.Path)))
	require.NoError(t, err)
	got := sha256.Sum256(installed)
	assert.Equal(t, hex.EncodeToString(sum[:]), hex.EncodeToString(got[:]))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, ranges, 2)
	assert.Empty(t, ranges[0])
	assert.True(t, strings.HasPrefix(ranges[1], "bytes=") && ranges[1] != "bytes=0-", ranges[1])
}
