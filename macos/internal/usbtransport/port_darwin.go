//go:build darwin

package usbtransport

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func openSerialPort(path string) (port *os.File, result error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			_ = syscall.Close(fd)
		}
	}()
	var settings syscall.Termios
	if err := serialIOCTL(fd, syscall.TIOCGETA, unsafe.Pointer(&settings)); err != nil {
		return nil, fmt.Errorf("read serial settings: %w", err)
	}
	settings.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK |
		syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	settings.Oflag &^= syscall.OPOST
	settings.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON |
		syscall.ISIG | syscall.IEXTEN
	settings.Cflag &^= syscall.CSIZE | syscall.PARENB
	settings.Cflag |= syscall.CS8 | syscall.CLOCAL | syscall.CREAD
	settings.Cc[syscall.VMIN] = 0
	settings.Cc[syscall.VTIME] = 1
	settings.Ispeed = 115200
	settings.Ospeed = 115200
	if err := serialIOCTL(fd, syscall.TIOCSETA, unsafe.Pointer(&settings)); err != nil {
		return nil, fmt.Errorf("apply serial settings: %w", err)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		return nil, fmt.Errorf("set blocking serial mode: %w", err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

func serialIOCTL(fd int, request uint, argument unsafe.Pointer) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), uintptr(request),
		uintptr(argument), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
