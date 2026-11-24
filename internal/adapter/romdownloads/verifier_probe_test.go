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
	adapter.RomJobWorker
	beats, complete, fail, retry, release int
	at                                    time.Time
	lost                                  bool
}

func (q *verifierQueue) Heartbeat(context.Context, adapter.RomJobLease, int64, int64, time.Duration) (adapter.RomJobLease, error) {
	q.beats++
	if q.lost {
		return adapter.RomJobLease{}, rom.ErrLeaseLost
	}
	return adapter.RomJobLease{}, nil
}

func (q *verifierQueue) Complete(context.Context, adapter.RomJobLease, rom.InstallResult) error {
	q.complete++
	return nil
}

func (q *verifierQueue) Fail(context.Context, adapter.RomJobLease, error) error {
	q.fail++
	return nil
}

func (q *verifierQueue) Retry(_ context.Context, _ adapter.RomJobLease, _ error, at time.Time) error {
	q.retry++
	q.at = at
	return nil
}

func (q *verifierQueue) Release(context.Context, adapter.RomJobLease) error {
	q.release++
	return nil
}

func verifierClaim() adapter.RomJobClaim {
	return adapter.RomJobClaim{
		JobClaim: rom.JobClaim{Job: rom.Job{ID: "1"}, Attempt: 1},
		Lease:    adapter.RomJobLease{JobID: "1"},
	}
}

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
	d := NewDownloads(l, JobPorts{Worker: q}, DefaultPolicy())
	defer d.cancel()
	d.policy.ProgressEvery = time.Millisecond
	d.run(verifierClaim())
	if !cancelled || q.beats == 0 || q.complete+q.fail+q.retry+q.release != 0 {
		t.Fatalf("cancelled %v queue %+v", cancelled, q)
	}
}

func TestVerifierRetryPolicy(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		q := &verifierQueue{}
		d := NewDownloads(nil, JobPorts{Worker: q}, DefaultPolicy())
		now := time.Unix(100, 0)
		d.now = func() time.Time { return now }
		cause := errors.New("transient")
		if permanent {
			cause = errors.Join(cause, rom.ErrNotRetryable)
		}
		d.finish(context.Background(), verifierClaim(), verifierClaim().Lease, rom.InstallResult{}, cause)
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
