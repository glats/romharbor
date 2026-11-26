package web_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestDownloadGameBundleResolvesReferencesAfterExtraction(t *testing.T) {
	root := t.TempDir()
	jobs := newJobs(t)
	library := &romlibrary.Library{Root: root}
	h := web.NewHandler(service.NewApp(nil, nil, nil, jobs,
		service.WithGameInspector(jobs, library)))
	job := queueJob(t, jobs, "Game (Disc 1 of 2)")
	claim, ok, err := jobs.Claim(t.Context(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, jobs.Complete(t.Context(), claim.Lease, rom.InstallResult{Path: "psx/Game"}))
	original := filepath.Join(root, "psx", "Game")
	const (
		playlist = "Game.m3u"
		discOne  = "Game (Disc 1)/disc.cue"
		discTwo  = "Game (Disc 2)/disc.cue"
	)
	contents := map[string]string{
		playlist:                        playlistBody(discOne, discTwo),
		discOne:                         "FILE \"tracks/data.bin\" BINARY\n",
		discTwo:                         "FILE \"tracks/data.bin\" BINARY\n",
		"Game (Disc 1)/tracks/data.bin": "disc one bytes",
		"Game (Disc 2)/tracks/data.bin": "disc two bytes",
	}
	for name, body := range contents {
		writeBundleTestFile(t, original, name, body)
	}
	writeBundleTestFile(t, original, "unrelated.bin", "must not be exported")

	target := "/api/job/bundle?id=" + job.ID
	rec := get(t, h, target, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	zipped, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	require.Len(t, zipped.File, len(contents))
	extracted := filepath.Join(t.TempDir(), "psx")
	for _, member := range zipped.File {
		expected, exists := contents[member.Name]
		require.True(t, exists, member.Name)
		rc, err := member.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		assert.Equal(t, expected, string(body))
		writeBundleTestFile(t, extracted, member.Name, string(body))
	}
	copied := (&romlibrary.Library{Root: filepath.Dir(extracted)}).InspectGame(t.Context(), rom.Job{
		Path: "psx/" + playlist, Title: job.Title, Status: rom.JobDone,
	})
	assert.Equal(t, rom.ReadinessComplete, copied.Status)
	assert.Len(t, copied.Files, len(contents))
	assert.Empty(t, copied.MissingFiles)
	assert.Empty(t, copied.MissingDiscs)

	require.NoError(t, os.Remove(filepath.Join(original, "Game (Disc 2)", "tracks", "data.bin")))
	incomplete := get(t, h, target, "")
	require.Equal(t, http.StatusConflict, incomplete.Code)
	assert.NotContains(t, incomplete.Header().Get("Content-Type"), "application/zip")
}

func playlistBody(entries ...string) string {
	return strings.Join(entries, "\n") + "\n"
}

func writeBundleTestFile(t *testing.T, root, name, body string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte(body), 0o644))
}
