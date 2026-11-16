package service

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"app.ddd/internal/adapter"
	"app.ddd/internal/app"
	"app.ddd/internal/app/catalog"
	"app.ddd/internal/app/command"
	"app.ddd/internal/app/mock"
	"app.ddd/internal/app/query"
	"app.ddd/internal/domain/user"
)

func TestNewAppIsTheOnlyApplicationConstructor(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "app.go", nil, 0)
	if !assert.NoError(t, err) {
		return
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && strings.HasPrefix(function.Name.Name, "NewApp") {
			assert.Equal(t, "NewApp", function.Name.Name)
		}
	}
}

func TestNewAppOnlyReturnsApplication(t *testing.T) {
	constructor := reflect.TypeOf(NewApp)
	assert.Equal(t, 1, constructor.NumOut())
	assert.Equal(t, reflect.TypeFor[*app.Application](), constructor.Out(0))
	assert.Equal(t, reflect.TypeFor[func(adapter.UserRepository, *catalog.Listings, *catalog.Downloads, adapter.RomJobStore, adapter.RomLibrary) *app.Application](), constructor)
}

func TestNewAppWiresEveryHandler(t *testing.T) {
	downloads := catalog.NewDownloads(nil, nil, nil)
	application := NewApp(mock.NewUserRepository(t), catalog.NewListings(nil, nil, 0), downloads, nil, nil)
	t.Cleanup(func() { assert.NoError(t, downloads.Shutdown(context.Background())) })

	assert.NotNil(t, application.Command.CreateUser)
	assert.NotNil(t, application.Command.UpdateUser)
	assert.NotNil(t, application.Command.DeleteUser)
	assert.NotNil(t, application.Query.ObtainUser)
	assert.NotNil(t, application.Query.ListUsers)
	assert.NotNil(t, application.Command.StartDownload)
	assert.NotNil(t, application.Query.ListPlatforms)
	assert.NotNil(t, application.Query.ListGames)
	assert.NotNil(t, application.Query.ObtainGame)
	assert.NotNil(t, application.Query.ListJobs)
	assert.NotNil(t, application.Query.ObtainJob)
	assert.NotNil(t, application.Query.ObtainDestination)
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

func TestNewAppHandlersUseTheGivenRepository(t *testing.T) {
	ctx := context.Background()
	ada := user.New(user.WithID("u1"), user.WithName("Ada"), user.WithEmail("ada@example.com"))
	repo := mock.NewUserRepository(t)
	repo.EXPECT().FindByID(ctx, "u1").Return(ada, nil).Times(2)
	repo.EXPECT().Delete(ctx, "u1").Return(nil).Once()
	repo.EXPECT().List(ctx).Return([]user.User{ada}, nil).Once()

	downloads := catalog.NewDownloads(nil, nil, nil)
	application := NewApp(repo, catalog.NewListings(nil, nil, 0), downloads, nil, nil)
	t.Cleanup(func() { assert.NoError(t, downloads.Shutdown(ctx)) })

	got, err := application.Query.ObtainUser.Handle(ctx, query.ObtainUser{ID: "u1"})
	assert.NoError(t, err)
	assert.Equal(t, ada, got)

	users, err := application.Query.ListUsers.Handle(ctx, query.ListUsers{})
	assert.NoError(t, err)
	assert.Equal(t, []user.User{ada}, users)

	assert.NoError(t, application.Command.DeleteUser.Handle(ctx, command.DeleteUser{ID: "u1"}))
}
