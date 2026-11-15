package service

import (
	"app.ddd/internal/adapter"
	"app.ddd/internal/app"
	"app.ddd/internal/app/catalog"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/query"
	"app.ddd/internal/common/metric"
)

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

func NewAppWithCatalog(userRepository adapter.UserRepository, romDeps catalog.Dependencies) *app.Application {
	application := NewApp(userRepository)
	application.Catalog = catalog.NewService(romDeps)
	return application
}
