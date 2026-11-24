package web

import "app.ddd/internal/domain/rom"

type sectionView struct {
	Flash   string
	Users   []userView
	Form    formView
	Editing *editView
}

type userView struct {
	ID    string
	Name  string
	Email string
}

type formView struct {
	Name  string
	Email string
	Error string
}

type editView struct {
	ID    string
	Name  string
	Email string
	Error string
}

type errorView struct {
	Flash string
}

// gameView is one game as the catalog page shows it. Labels and Compatible
// are the slot the compatibility badges occupy; they stay empty until the
// classifier feeds them.
type gameView struct {
	ID         string
	Title      string
	Region     string
	Size       int64
	Cover      string
	Source     string
	Platform   string
	PageURL    string
	Files      []rom.File
	Labels     []string
	Compatible *bool
}

// jobView is one download as the downloads panel shows it.
type jobView struct {
	ID         string
	Title      string
	Status     string
	Platform   string
	Downloaded int64
	Total      int64
	Percent    int
	Error      string
	Path       string
}
