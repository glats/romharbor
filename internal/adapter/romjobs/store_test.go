package romjobs

import (
	"sync"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestAddAndGetReturnSnapshots(t *testing.T) {
	store := New()
	added := store.Add(rom.Job{ID: "1", Title: "Game", Status: "queued"})

	added.Status = "mutated by caller"
	got, ok := store.Get("1")
	if !ok || got.Status != "queued" {
		t.Fatalf("Get() = (%+v, %v), want the stored queued snapshot", got, ok)
	}
	got.Status = "mutated by reader"
	again, _ := store.Get("1")
	if again.Status != "queued" {
		t.Errorf("stored status = %q after reader mutation, want queued", again.Status)
	}
}

func TestGetQueuedKeepsTheInitialSnapshot(t *testing.T) {
	store := New()
	store.Add(rom.Job{ID: "1", Status: "queued", Total: 10})
	store.Update("1", func(j *rom.Job) {
		j.Status = "done"
		j.Downloaded = 10
	})

	queued, ok := store.GetQueued("1")
	if !ok || queued.Status != "queued" || queued.Downloaded != 0 || queued.Total != 10 {
		t.Fatalf("GetQueued() = (%+v, %v), want the initial queued snapshot", queued, ok)
	}
	if _, ok := store.GetQueued("missing"); ok {
		t.Error("GetQueued() found a job that was never added")
	}
}

func TestUpdateIsVisibleOnlyThroughSnapshots(t *testing.T) {
	store := New()
	store.Add(rom.Job{ID: "1", Status: "queued"})
	before, _ := store.Get("1")

	store.Update("1", func(j *rom.Job) { j.Status = "running" })

	if before.Status != "queued" {
		t.Errorf("earlier snapshot changed to %q", before.Status)
	}
	if after, _ := store.Get("1"); after.Status != "running" {
		t.Errorf("status = %q, want running", after.Status)
	}
	if _, ok := store.Get("missing"); ok {
		t.Error("Get() found a job that was never added")
	}
}

func TestConcurrentUpdatesAndReadsDoNotRace(t *testing.T) {
	store := New()
	store.Add(rom.Job{ID: "1"})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			store.Update("1", func(j *rom.Job) { j.Downloaded++ })
		}()
		go func() {
			defer wg.Done()
			_, _ = store.Get("1")
			_ = store.List()
		}()
	}
	wg.Wait()

	if job, _ := store.Get("1"); job.Downloaded != 50 {
		t.Errorf("Downloaded = %d, want 50", job.Downloaded)
	}
}
