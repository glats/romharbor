package service

import (
	"app.ddd/internal/adapter"
	"app.ddd/internal/app"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/common/metric"
)

func NewApp(userRepository adapter.UserRepository, listings adapter.RomListings, downloads adapter.RomDownloads, jobs adapter.RomJobStore) *app.Application {
	metrics := metric.NoOp{}

	return &app.Application{
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
}
