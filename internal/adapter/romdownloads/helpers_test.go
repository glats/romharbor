package romdownloads

import (
	"context"

	"app.ddd/internal/domain/rom"
)

type blockingLibrary struct {
	started chan struct{}
}

func (b *blockingLibrary) PlatformDir(p rom.Platform) (string, error) {
	return "/unused/" + p.Slug, nil
}

func (b *blockingLibrary) Install(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
	close(b.started)
	<-ctx.Done()
	return rom.InstallResult{}, ctx.Err()
}
