package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"app.ddd/internal/app"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/mock"
	"app.ddd/internal/app/query"
	"app.ddd/internal/domain/user"
)

func TestNewAppWiresEveryHandler(t *testing.T) {
	application := NewApp(mock.NewUserRepository(t))

	assert.NotNil(t, application.Command.CreateUser)
	assert.NotNil(t, application.Command.UpdateUser)
	assert.NotNil(t, application.Command.DeleteUser)
	assert.NotNil(t, application.Query.ObtainUser)
	assert.NotNil(t, application.Query.ListUsers)
}

func TestApplicationHasNoParallelCatalogService(t *testing.T) {
	_, found := reflect.TypeOf(app.Application{}).FieldByName("Catalog")
	assert.False(t, found, "catalog use cases must be reached through Command and Query")
	for _, name := range []string{"StartDownload"} {
		_, ok := reflect.TypeOf(app.Command{}).FieldByName(name)
		assert.True(t, ok, "Command."+name)
	}
	for _, name := range []string{"ListPlatforms", "ListGames", "ObtainGame", "ListJobs", "ObtainJob", "ObtainDestination"} {
		_, ok := reflect.TypeOf(app.Query{}).FieldByName(name)
		assert.True(t, ok, "Query."+name)
	}
}

func TestNewAppWithCatalogWiresCatalogUseCases(t *testing.T) {
	application, shutdown := NewAppWithCatalog(mock.NewUserRepository(t), CatalogDependencies{})
	assert.NotNil(t, shutdown)
	assert.NotNil(t, application.Command.StartDownload)
	assert.NotNil(t, application.Query.ListPlatforms)
	assert.NotNil(t, application.Query.ObtainDestination)
	assert.Nil(t, NewApp(mock.NewUserRepository(t)).Query.ListPlatforms)
}

func TestNewAppHandlersUseTheGivenRepository(t *testing.T) {
	ctx := context.Background()
	ada := user.New(user.WithID("u1"), user.WithName("Ada"), user.WithEmail("ada@example.com"))
	repo := mock.NewUserRepository(t)
	repo.EXPECT().FindByID(ctx, "u1").Return(ada, nil).Times(2)
	repo.EXPECT().Delete(ctx, "u1").Return(nil).Once()
	repo.EXPECT().List(ctx).Return([]user.User{ada}, nil).Once()

	application := NewApp(repo)

	got, err := application.Query.ObtainUser.Handle(ctx, query.ObtainUser{ID: "u1"})
	assert.NoError(t, err)
	assert.Equal(t, ada, got)

	users, err := application.Query.ListUsers.Handle(ctx, query.ListUsers{})
	assert.NoError(t, err)
	assert.Equal(t, []user.User{ada}, users)

	assert.NoError(t, application.Command.DeleteUser.Handle(ctx, command.DeleteUser{ID: "u1"}))
}
