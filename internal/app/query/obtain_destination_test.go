package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

func TestObtainDestinationReturnsTheLibraryFolderOfThePlatform(t *testing.T) {
	ctx := context.Background()
	handler := NewObtainDestinationHandler(metric.NoOp{})

	genesis, _ := rom.PlatformBySlug("genesis")
	folder, err := handler.Handle(ctx, ObtainDestination{Platform: genesis})
	require.NoError(t, err)
	assert.Equal(t, "megadrive", folder)

	_, err = handler.Handle(ctx, ObtainDestination{})
	assert.ErrorIs(t, err, rom.ErrNoLibraryFolder)
}
