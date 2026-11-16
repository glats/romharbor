package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainDestination struct {
	Platform rom.Platform
}

type ObtainDestinationHandler decorator.QueryHandler[ObtainDestination, string]

type obtainDestinationQuery struct {
	library adapter.RomLibrary
}

func NewObtainDestinationHandler(library adapter.RomLibrary, metric decorator.MetricsClient) ObtainDestinationHandler {
	return decorator.ApplyQueryDecorators[ObtainDestination, string](obtainDestinationQuery{library: library}, metric)
}

func (q obtainDestinationQuery) Handle(_ context.Context, query ObtainDestination) (string, error) {
	return q.library.PlatformDir(query.Platform)
}
