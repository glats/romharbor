package app

import (
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/domain/user"
)

type Application struct {
	Command *Command
	Query   *Query
}

type Command struct {
	CreateUser    decorator.CommandHandler[command.CreateUser]
	UpdateUser    decorator.CommandHandler[command.UpdateUser]
	DeleteUser    decorator.CommandHandler[command.DeleteUser]
	StartDownload decorator.CommandResultHandler[command.StartDownload, rom.Job]
}

type Query struct {
	ObtainUser          decorator.QueryHandler[query.ObtainUser, user.User]
	ListUsers           decorator.QueryHandler[query.ListUsers, []user.User]
	ListPlatforms       decorator.QueryHandler[query.ListPlatforms, []rom.PlatformCount]
	BrowsePlatformGames decorator.QueryHandler[query.BrowsePlatformGames, rom.GamePage]
	SearchGames         decorator.QueryHandler[query.SearchGames, rom.GamePage]
	SearchAllGames      decorator.QueryHandler[query.SearchAllGames, query.SearchAllGamesResult]
	ObtainGame          decorator.QueryHandler[query.ObtainGame, rom.Game]
	ListJobs            decorator.QueryHandler[query.ListJobs, []rom.Job]
	ObtainJob           decorator.QueryHandler[query.ObtainJob, rom.Job]
	ObtainCover         decorator.QueryHandler[query.ObtainCover, rom.Cover]
}
