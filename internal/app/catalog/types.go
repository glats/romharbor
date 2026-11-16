package catalog

import (
	"time"

	"app.ddd/internal/domain/rom"
)

const DefaultCacheTTL = 24 * time.Hour

type PlatformCount struct {
	rom.Platform
	Count int
}
