//go:build !darwin

package usbtransport

import (
	"errors"
	"os"
)

func openSerialPort(string) (*os.File, error) {
	return nil, errors.New("USB serial transport is supported on macOS only")
}
