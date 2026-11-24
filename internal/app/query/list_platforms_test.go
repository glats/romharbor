package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

type recordingMetrics struct {
	keys []string
}

func (r *recordingMetrics) Inc(key string, _ int) {
	r.keys = append(r.keys, key)
}

func TestListPlatformsCountsEachPlatformListing(t *testing.T) {
	platforms, err := NewListPlatformsHandler(newListings(
		rom.Game{ID: "1", Platform: "snes", Title: "Zelda"},
		rom.Game{ID: "2", Platform: "snes", Title: "Mario"},
	), metric.NoOp{}).Handle(context.Background(), ListPlatforms{})

	require.NoError(t, err)
	counts := map[string]int{}
	for _, p := range platforms {
		counts[p.Slug] = p.Count
	}
	assert.Equal(t, 2, counts["snes"])
	assert.Len(t, platforms, len(rom.Platforms()))
}

func TestCatalogQueriesReportDecoratorMetrics(t *testing.T) {
	ctx := context.Background()
	listings := newListings(rom.Game{ID: "1", Platform: "snes", Title: "Zelda"})
	jobs := fakeJobs{jobs: []rom.Job{{ID: "1"}}}
	library := fakeLibrary{}
	snes, _ := rom.PlatformBySlug("snes")
	metrics := &recordingMetrics{}

	calls := map[string]func() error{
		"querys.listplatforms.success": func() error {
			_, err := NewListPlatformsHandler(listings, metrics).Handle(ctx, ListPlatforms{})
			return err
		},
		"querys.browseplatformgames.success": func() error {
			_, err := NewBrowsePlatformGamesHandler(listings, metrics).Handle(ctx, BrowsePlatformGames{Platform: "snes", Limit: 1})
			return err
		},
		"querys.searchgames.success": func() error {
			_, err := NewSearchGamesHandler(listings, metrics).Handle(ctx, SearchGames{Text: "zelda", Platform: "snes", Limit: 1})
			return err
		},
		"querys.obtaingame.success": func() error {
			_, err := NewObtainGameHandler(listings, metrics).Handle(ctx, ObtainGame{ID: "1"})
			return err
		},
		"querys.listjobs.success": func() error {
			_, err := NewListJobsHandler(jobs, metrics).Handle(ctx, ListJobs{})
			return err
		},
		"querys.obtainjob.success": func() error {
			_, err := NewObtainJobHandler(jobs, metrics).Handle(ctx, ObtainJob{ID: "1"})
			return err
		},
		"querys.obtaindestination.success": func() error {
			_, err := NewObtainDestinationHandler(library, metrics).Handle(ctx, ObtainDestination{Platform: snes})
			return err
		},
	}
	for key, call := range calls {
		require.NoError(t, call(), key)
		assert.Contains(t, metrics.keys, key)
	}
}
