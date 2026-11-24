package query

import (
	"context"

	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainDestination struct {
	Platform rom.Platform
}

type ObtainDestinationHandler decorator.QueryHandler[ObtainDestination, string]

type obtainDestinationQuery struct{}

func NewObtainDestinationHandler(metric decorator.MetricsClient) ObtainDestinationHandler {
	return decorator.ApplyQueryDecorators[ObtainDestination, string](obtainDestinationQuery{}, metric)
}

func (obtainDestinationQuery) Handle(_ context.Context, query ObtainDestination) (string, error) {
	return query.Platform.Destination()
}
