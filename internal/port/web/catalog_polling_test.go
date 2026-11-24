package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func newPollingHandler(t *testing.T) (http.Handler, *sqlite.JobStore) {
	t.Helper()
	jobs := newJobs(t)
	application := service.NewApp(nil, nil, nil, jobs, nil)
	return web.NewHandler(application), jobs
}

func get(t *testing.T, h http.Handler, path, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func queueJob(t *testing.T, jobs *sqlite.JobStore, id string) rom.Job {
	t.Helper()
	file := rom.File{Name: id + ".sfc", Size: 100, URL: "http://unused.invalid/" + id}
	platform, _ := rom.PlatformBySlug("snes")
	job, err := jobs.Enqueue(context.Background(), rom.InstallRequest{
		Game:     rom.Game{ID: id, Platform: "snes", Title: id, Files: []rom.File{file}},
		File:     file,
		Platform: platform,
	})
	require.NoError(t, err)
	return job
}

func TestJobPollingAnswersNotModifiedWhileTheStateIsUnchanged(t *testing.T) {
	h, jobs := newPollingHandler(t)
	job := queueJob(t, jobs, "a")
	for _, path := range []string{"/api/job?id=" + job.ID, "/api/jobs"} {
		t.Run(path, func(t *testing.T) {
			first := get(t, h, path, "")
			require.Equal(t, http.StatusOK, first.Code)
			etag := first.Header().Get("ETag")
			require.NotEmpty(t, etag)
			assert.Equal(t, "no-cache", first.Header().Get("Cache-Control"))
			assert.Equal(t, "application/json", first.Header().Get("Content-Type"))

			same := get(t, h, path, etag)

			assert.Equal(t, http.StatusNotModified, same.Code)
			assert.Empty(t, same.Body.String())
			assert.Equal(t, etag, same.Header().Get("ETag"))
			assert.Equal(t, "no-cache", same.Header().Get("Cache-Control"))
		})
	}
}

func TestJobPollingReturnsTheNewStateOnceItChanges(t *testing.T) {
	h, jobs := newPollingHandler(t)
	job := queueJob(t, jobs, "a")
	path := "/api/job?id=" + job.ID
	etag := get(t, h, path, "").Header().Get("ETag")

	claim, ok, err := jobs.Claim(context.Background(), time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = jobs.Heartbeat(context.Background(), claim.Lease, 40, 100, time.Minute)
	require.NoError(t, err)
	changed := get(t, h, path, etag)

	require.Equal(t, http.StatusOK, changed.Code)
	assert.NotEqual(t, etag, changed.Header().Get("ETag"))
	body := decodeJSON(t, changed)
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, float64(40), body["downloaded"])
}

func TestJobPollingMatchesAnyListedOrWeakValidator(t *testing.T) {
	h, jobs := newPollingHandler(t)
	job := queueJob(t, jobs, "a")
	path := "/api/job?id=" + job.ID
	etag := get(t, h, path, "").Header().Get("ETag")

	for name, header := range map[string]string{
		"list":     `"other", ` + etag,
		"weak":     "W/" + etag,
		"wildcard": "*",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusNotModified, get(t, h, path, header).Code)
		})
	}
	assert.Equal(t, http.StatusOK, get(t, h, path, `"unrelated"`).Code)
}

func TestJobPollingErrorsAreNeverRevalidated(t *testing.T) {
	h, _ := newPollingHandler(t)

	rec := get(t, h, "/api/job?id=missing", "*")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":"job not found"}`, rec.Body.String())
	assert.Empty(t, rec.Header().Get("ETag"))
}
