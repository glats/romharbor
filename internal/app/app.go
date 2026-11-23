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
	StartDownload decorator.CommandHandler[command.StartDownload]
}

type Query struct {
	ObtainUser        decorator.QueryHandler[query.ObtainUser, user.User]
	ListUsers         decorator.QueryHandler[query.ListUsers, []user.User]
	ListPlatforms     decorator.QueryHandler[query.ListPlatforms, []rom.PlatformCount]
	ListGames         decorator.QueryHandler[query.ListGames, rom.GamePage]
	ObtainGame        decorator.QueryHandler[query.ObtainGame, rom.Game]
	ListJobs          decorator.QueryHandler[query.ListJobs, []rom.Job]
	ObtainJob         decorator.QueryHandler[query.ObtainJob, rom.Job]
	ObtainDestination decorator.QueryHandler[query.ObtainDestination, string]
}
