//go:build !windows

package update

import (
	"context"
	"errors"
)

func Apply() error { return errors.New("automatic updates are only supported on Windows") }

func waitForHelperReady(context.Context, int, string) error {
	return errors.New("automatic updates are only supported on Windows")
}
