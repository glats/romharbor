package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainCover struct {
	Platform string
	Title    string
}

type ObtainCoverHandler decorator.QueryHandler[ObtainCover, rom.Cover]

type obtainCoverQuery struct {
	covers adapter.RomCovers
}

func NewObtainCoverHandler(covers adapter.RomCovers, metric decorator.MetricsClient) ObtainCoverHandler {
	return decorator.ApplyQueryDecorators[ObtainCover, rom.Cover](obtainCoverQuery{covers: covers}, metric)
}

func (q obtainCoverQuery) Handle(ctx context.Context, query ObtainCover) (rom.Cover, error) {
	return q.covers.Cover(ctx, query.Platform, query.Title)
}
