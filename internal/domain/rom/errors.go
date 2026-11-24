package rom

import "errors"

var (
	ErrGameNotFound    = errors.New("game not found")
	ErrNoDownloadFile  = errors.New("game has no file")
	ErrUnknownPlatform = errors.New("unknown platform")
	ErrNoLibraryFolder = errors.New("platform has no library folder")
	ErrShuttingDown    = errors.New("catalog is shutting down")
	ErrJobNotFound     = errors.New("job not found")
	ErrNotRetryable    = errors.New("retrying cannot fix this failure")
	ErrLeaseLost       = errors.New("job lease lost")
)
