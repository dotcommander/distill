//go:build !darwin && !linux && !windows

package cmd

import (
	"errors"
	"runtime"
)

func renameNoReplace(_, _ string) error {
	return errors.New("atomic no-replace directory publication is unsupported on " + runtime.GOOS)
}
