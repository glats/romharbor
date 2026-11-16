package service

import (
	"context"
	"log/slog"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/app"
	"app.ddd/internal/app/catalog"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/common/metric"
)

type CatalogDependencies struct {
	Sources []adapter.RomSource
	Cache   adapter.RomListingCache
	Library adapter.RomLibrary
	Jobs    adapter.RomJobStore
	TTL     time.Duration
	Logger  *slog.Logger
}

func NewApp(userRepository adapter.UserRepository) *app.Application {
	metrics := metric.NoOp{}

	return &app.Application{
		Command: &app.Command{
			CreateUser: command.NewCreateUserHandler(userRepository, metrics),
			UpdateUser: command.NewUpdateUserHandler(userRepository, metrics),
			DeleteUser: command.NewDeleteUserHandler(userRepository, metrics),
		},
		Query: &app.Query{
			ObtainUser: query.NewObtainUserHandler(userRepository, metrics),
			ListUsers:  query.NewListUsersHandler(userRepository, metrics),
		},
	}
}

func NewAppWithCatalog(userRepository adapter.UserRepository, deps CatalogDependencies) (*app.Application, func(context.Context) error) {
	metrics := metric.NoOp{}
	application := NewApp(userRepository)
	listings := catalog.NewListings(deps.Sources, deps.Cache, deps.TTL)
	downloads := catalog.NewDownloads(deps.Library, deps.Jobs, deps.Logger)

	application.Command.StartDownload = command.NewStartDownloadHandler(listings, downloads, metrics)
	application.Query.ListPlatforms = query.NewListPlatformsHandler(listings, metrics)
	application.Query.ListGames = query.NewListGamesHandler(listings, metrics)
	application.Query.ObtainGame = query.NewObtainGameHandler(listings, metrics)
	application.Query.ListJobs = query.NewListJobsHandler(deps.Jobs, metrics)
	application.Query.ObtainJob = query.NewObtainJobHandler(deps.Jobs, metrics)
	application.Query.ObtainDestination = query.NewObtainDestinationHandler(deps.Library, metrics)

	return application, downloads.Shutdown
}
