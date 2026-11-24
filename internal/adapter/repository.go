package adapter

import (
	"context"
	"time"

	"app.ddd/internal/domain/rom"
	"app.ddd/internal/domain/user"
)

type UserRepository interface {
	Create(ctx context.Context, u user.User) error
	Update(ctx context.Context, u user.User) error
	Delete(ctx context.Context, id string) error
	FindByID(ctx context.Context, id string) (user.User, error)
	FindByEmail(ctx context.Context, email string) (user.User, error)
	List(ctx context.Context) ([]user.User, error)
}

type RomSource interface {
	Name() string
	Platforms() []string
	Search(ctx context.Context, q rom.Query) ([]rom.Game, error)
}

// RomSearchOnlySource is a source that cannot list a platform: it answers text
// searches only, and is asked nothing else.
type RomSearchOnlySource interface {
	RomSource
	SearchOnly() bool
}

// RomGameResolver is a source that can rebuild a game from its ID alone, so a
// game found by a search stays reachable after a restart.
type RomGameResolver interface {
	ResolveGame(id string) (rom.Game, bool)
}

type RomListingCache interface {
	Load(platform string) (rom.Listing, error)
	Store(platform string, listing rom.Listing) error
}

type RomLibrary interface {
	PlatformDir(p rom.Platform) (string, error)
	Install(ctx context.Context, req rom.InstallRequest, progress func(downloaded, total int64)) (rom.InstallResult, error)
}

type RomJobStore interface {
	Get(ctx context.Context, id string) (rom.Job, bool, error)
	List(ctx context.Context) ([]rom.Job, error)
}

type RomJobQueue interface {
	Enqueue(ctx context.Context, req rom.InstallRequest) (rom.Job, error)
	Claim(ctx context.Context, lease time.Duration) (rom.JobClaim, bool, error)
	Heartbeat(ctx context.Context, id string, downloaded, total int64, lease time.Duration) error
	Complete(ctx context.Context, id string, result rom.InstallResult) error
	Retry(ctx context.Context, id string, cause error, runAt time.Time) error
	Fail(ctx context.Context, id string, cause error) error
	Release(ctx context.Context, id string) error
	Recover(ctx context.Context) (int, error)
	Prune(ctx context.Context, finishedBefore time.Time) (int, error)
}

type RomListings interface {
	PlatformCounts(ctx context.Context) ([]rom.PlatformCount, error)
	ObtainPlatformListing(ctx context.Context, platform string) (rom.GamePage, error)
	SearchSources(ctx context.Context, text, platform string) (rom.GamePage, error)
	Game(ctx context.Context, id string) (rom.Game, bool, error)
}

type RomDownloads interface {
	Start(ctx context.Context, req rom.InstallRequest) (rom.Job, error)
}
