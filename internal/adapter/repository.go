package adapter

import (
	"context"

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

type RomListingCache interface {
	Load(platform string) (rom.Listing, error)
	Store(platform string, listing rom.Listing) error
}

type RomLibrary interface {
	PlatformDir(p rom.Platform) (string, error)
	Install(ctx context.Context, req rom.InstallRequest, progress func(downloaded, total int64)) (rom.InstallResult, error)
}

type RomJobStore interface {
	Add(job rom.Job) rom.Job
	Get(id string) (rom.Job, bool)
	GetQueued(id string) (rom.Job, bool)
	Update(id string, change func(*rom.Job))
	List() []rom.Job
}

type RomListings interface {
	PlatformCounts(ctx context.Context) []rom.PlatformCount
	Games(ctx context.Context, platform, text string, offset, limit int) ([]rom.Game, error)
	Game(ctx context.Context, id string) (rom.Game, bool, error)
}

type RomDownloads interface {
	Start(game rom.Game, file rom.File, platform rom.Platform, newJobID func() string) error
}
