package romjobs

import (
	"sync"

	"app.ddd/internal/domain/rom"
)

type Store struct {
	mu     sync.Mutex
	jobs   map[string]*rom.Job
	queued map[string]rom.Job
}

func New() *Store {
	return &Store{jobs: map[string]*rom.Job{}, queued: map[string]rom.Job{}}
}

func (s *Store) Add(job rom.Job) rom.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := job
	s.jobs[stored.ID] = &stored
	s.queued[stored.ID] = job
	return stored
}

func (s *Store) Get(id string) (rom.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return rom.Job{}, false
	}
	return *job, true
}

func (s *Store) GetQueued(id string) (rom.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.queued[id]
	return job, ok
}

func (s *Store) Update(id string, change func(*rom.Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		change(job)
	}
}

func (s *Store) List() []rom.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]rom.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, *job)
	}
	return out
}
