//go:build !windows

package nativecapture

import (
	"errors"
	"os/exec"
)

func supported() bool       { return false }
func hideProcess(*exec.Cmd) {}
func sources() ([]Source, error) {
	return nil, errors.New("На этой платформе используется браузерный захват.")
}
