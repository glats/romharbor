package query

import (
	"context"

	"app.ddd/internal/domain/rom"
)

type fakeJobs struct {
	jobs []rom.Job
}

func (f fakeJobs) Get(_ context.Context, id string) (rom.Job, bool, error) {
	for _, job := range f.jobs {
		if job.ID == id {
			return job, true, nil
		}
	}
	return rom.Job{}, false, nil
}

func (f fakeJobs) List(context.Context) ([]rom.Job, error) {
	return f.jobs, nil
}
