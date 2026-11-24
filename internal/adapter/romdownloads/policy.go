package romdownloads

import "time"

const (
	defaultWorkers       = 1
	defaultLease         = 30 * time.Second
	defaultProgressEvery = time.Second
	defaultPoll          = 5 * time.Second
	defaultMaxAttempts   = 3
	defaultRetention     = 30 * 24 * time.Hour
	defaultTimeout       = 6 * time.Hour
)

// Policy is the retry, backoff and retention policy of the download runner.
type Policy struct {
	Workers       int
	Lease         time.Duration
	ProgressEvery time.Duration
	Poll          time.Duration
	MaxAttempts   int
	Backoff       func(attempt int) time.Duration
	Retention     time.Duration
	Timeout       time.Duration
}

// DefaultPolicy is the policy the download runner works with.
func DefaultPolicy() Policy {
	return Policy{
		Workers:       defaultWorkers,
		Lease:         defaultLease,
		ProgressEvery: defaultProgressEvery,
		Poll:          defaultPoll,
		MaxAttempts:   defaultMaxAttempts,
		Backoff:       defaultBackoff,
		Retention:     defaultRetention,
		Timeout:       defaultTimeout,
	}
}
