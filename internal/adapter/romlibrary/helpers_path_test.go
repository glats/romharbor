package romlibrary

import "path/filepath"

func (l *Library) abs(relative string) string {
	return filepath.Join(l.Root, filepath.FromSlash(relative))
}
