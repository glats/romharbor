package rom

import "errors"

var (
	ErrGameNotFound    = errors.New("game not found")
	ErrNoDownloadFile  = errors.New("game has no file")
	ErrUnknownPlatform = errors.New("unknown platform")
	ErrShuttingDown    = errors.New("catalog is shutting down")
	ErrJobNotFound     = errors.New("job not found")
)
