package service

import (
	"app.ddd/internal/adapter"
	"app.ddd/internal/app"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/common/metric"
)

type Option func(*app.Application, metric.NoOp)

func WithGameInspector(jobs adapter.RomJobStore, inspector adapter.RomGameInspector) Option {
	return func(application *app.Application, metrics metric.NoOp) {
		application.Query.ObtainGameReadiness = query.NewObtainGameReadinessHandler(jobs, inspector, metrics)
		application.Query.ObtainGameBundle = query.NewObtainGameBundleHandler(jobs, inspector, metrics)
	}
}

func WithCovers(covers adapter.RomCovers) Option {
	return func(application *app.Application, metrics metric.NoOp) {
		application.Query.ObtainCover = query.NewObtainCoverHandler(covers, metrics)
	}
}

func NewApp(userRepository adapter.UserRepository, listings adapter.RomListings, downloads adapter.RomDownloads, jobs adapter.RomJobStore, options ...Option) *app.Application {
	metrics := metric.NoOp{}

	application := &app.Application{
		Command: &app.Command{
			CreateUser:    command.NewCreateUserHandler(userRepository, metrics),
			UpdateUser:    command.NewUpdateUserHandler(userRepository, metrics),
			DeleteUser:    command.NewDeleteUserHandler(userRepository, metrics),
			StartDownload: command.NewStartDownloadHandler(listings, downloads, metrics),
		},
		Query: &app.Query{
			ObtainUser:          query.NewObtainUserHandler(userRepository, metrics),
			ListUsers:           query.NewListUsersHandler(userRepository, metrics),
			ListPlatforms:       query.NewListPlatformsHandler(listings, metrics),
			BrowsePlatformGames: query.NewBrowsePlatformGamesHandler(listings, metrics),
			SearchGames:         query.NewSearchGamesHandler(listings, metrics),
			SearchAllGames:      query.NewSearchAllGamesHandler(listings, metrics),
			ObtainGame:          query.NewObtainGameHandler(listings, metrics),
			ListJobs:            query.NewListJobsHandler(jobs, metrics),
			ObtainJob:           query.NewObtainJobHandler(jobs, metrics),
		},
	}
	for _, option := range options {
		option(application, metrics)
	}
	return application
}
