package rom

import (
	"context"
	"errors"
	"io"
)

var ErrBundleUnavailable = errors.New("a portable playlist bundle is not available")

type GameBundle struct {
	Name     string
	WriteZIP func(context.Context, io.Writer) error
}
