package query

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

type fakeLibrary struct {
	err error
}

func (f fakeLibrary) PlatformDir(p rom.Platform) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "/library/" + p.Slug, nil
}

func (fakeLibrary) Install(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
	return rom.InstallResult{}, errors.New("unused")
}

func TestObtainDestinationReturnsThePlatformFolderOrTheLibraryError(t *testing.T) {
	snes, _ := rom.PlatformBySlug("snes")
	ctx := context.Background()

	dir, err := NewObtainDestinationHandler(fakeLibrary{}, metric.NoOp{}).Handle(ctx, ObtainDestination{Platform: snes})
	require.NoError(t, err)
	assert.Equal(t, "/library/snes", dir)

	boom := errors.New("library unavailable")
	_, err = NewObtainDestinationHandler(fakeLibrary{err: boom}, metric.NoOp{}).Handle(ctx, ObtainDestination{Platform: snes})
	assert.ErrorIs(t, err, boom)
}
