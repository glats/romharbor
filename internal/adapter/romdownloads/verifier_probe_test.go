package romdownloads

import (
	"context"
	"errors"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type verifierQueue struct {
	adapter.RomJobQueue
	beats, complete, fail, retry, release int
	at                                    time.Time
	lost                                  bool
}

func (q *verifierQueue) Heartbeat(context.Context, string, int64, int64, time.Duration) error {
	q.beats++
	if q.lost {
		return rom.ErrLeaseLost
	}
	return nil
}

func (q *verifierQueue) Complete(context.Context, string, rom.InstallResult) error {
	q.complete++
	return nil
}
func (q *verifierQueue) Fail(context.Context, string, error) error { q.fail++; return nil }
func (q *verifierQueue) Retry(_ context.Context, _ string, _ error, at time.Time) error {
	q.retry++
	q.at = at
	return nil
}
func (q *verifierQueue) Release(context.Context, string) error { q.release++; return nil }
func TestVerifierRunnerLost(t *testing.T) {
	q := &verifierQueue{lost: true}
	cancelled := false
	l := &fakeLibrary{install: func(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
		select {
		case <-ctx.Done():
			cancelled = true
			return rom.InstallResult{}, ctx.Err()
		case <-time.After(time.Second):
			return rom.InstallResult{}, errors.New("worker did not stop")
		}
	}}
	d := NewDownloads(l, q, nil)
	defer d.cancel()
	d.ProgressEvery = time.Millisecond
	d.run(rom.JobClaim{Job: rom.Job{ID: "1"}, Attempt: 1})
	if !cancelled || q.beats == 0 || q.complete+q.fail+q.retry+q.release != 0 {
		t.Fatalf("cancelled %v queue %+v", cancelled, q)
	}
}

func TestVerifierRetryPolicy(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		q := &verifierQueue{}
		d := NewDownloads(nil, q, nil)
		now := time.Unix(100, 0)
		d.Now = func() time.Time { return now }
		cause := errors.New("transient")
		if permanent {
			cause = errors.Join(cause, rom.ErrNotRetryable)
		}
		d.finish(context.Background(), rom.JobClaim{Job: rom.Job{ID: "1"}, Attempt: 1}, rom.InstallResult{}, cause)
		d.cancel()
		if permanent {
			if q.fail != 1 || q.retry != 0 {
				t.Fatal(q)
			}
		} else if q.retry != 1 || !q.at.Equal(now.Add(30*time.Second)) {
			t.Fatal(q)
		}
	}
}
