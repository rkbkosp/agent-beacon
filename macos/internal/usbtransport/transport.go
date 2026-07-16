package usbtransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type MessageTransport interface {
	Name() string
	ReadMessage(context.Context) ([]byte, error)
	WriteMessage(context.Context, []byte) error
	Close() error
}

type FramedPort struct {
	name      string
	port      io.ReadWriteCloser
	decoder   Decoder
	pending   [][]byte
	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
}

func NewFramedPort(name string, port io.ReadWriteCloser) *FramedPort {
	return &FramedPort{name: name, port: port}
}

func (transport *FramedPort) Name() string { return transport.name }

func (transport *FramedPort) ReadMessage(ctx context.Context) ([]byte, error) {
	buffer := make([]byte, 4096)
	for {
		if len(transport.pending) > 0 {
			message := transport.pending[0]
			transport.pending = transport.pending[1:]
			return message, nil
		}
		count, err := transport.port.Read(buffer)
		if count > 0 {
			frames, _ := transport.decoder.Feed(buffer[:count])
			transport.pending = append(transport.pending, frames...)
			if len(transport.pending) > 0 {
				continue
			}
		}
		if errors.Is(err, io.EOF) && count == 0 && !transport.closed.Load() {
			err = nil
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

func (transport *FramedPort) WriteMessage(ctx context.Context, message []byte) error {
	wire, err := Encode(message)
	if err != nil {
		return err
	}
	transport.writeMu.Lock()
	defer transport.writeMu.Unlock()
	for written := 0; written < len(wire); {
		count, writeErr := transport.port.Write(wire[written:])
		written += count
		if writeErr != nil {
			return writeErr
		}
		if count == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	return nil
}

func (transport *FramedPort) Close() error {
	transport.closeOnce.Do(func() {
		transport.closed.Store(true)
		transport.closeErr = transport.port.Close()
	})
	return transport.closeErr
}

type Config struct {
	Enabled      bool
	Port         string
	ScanInterval time.Duration
}

func Run(ctx context.Context, config Config,
	serve func(context.Context, MessageTransport) error,
	logf func(string, ...any)) {
	if !config.Enabled {
		return
	}
	if config.Port == "" {
		config.Port = "/dev/cu.usbmodem*"
	}
	if config.ScanInterval <= 0 {
		config.ScanInterval = time.Second
	}
	for ctx.Err() == nil {
		ports, err := matchingPorts(config.Port)
		if err != nil {
			logf("USB port discovery failed: %v", err)
		} else {
			for _, path := range ports {
				port, openErr := openSerialPort(path)
				if openErr != nil {
					continue
				}
				logf("USB transport probing %s", path)
				transport := NewFramedPort("usb:"+path, port)
				serveErr := serve(ctx, transport)
				_ = transport.Close()
				if ctx.Err() != nil {
					return
				}
				if serveErr != nil && !errors.Is(serveErr, context.Canceled) &&
					!errors.Is(serveErr, io.EOF) {
					logf("USB transport %s disconnected: %v", path, serveErr)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(config.ScanInterval):
		}
	}
}

func matchingPorts(pattern string) ([]string, error) {
	if !hasGlobMeta(pattern) {
		if _, err := filepath.Glob(pattern); err != nil {
			return nil, err
		}
		return []string{pattern}, nil
	}
	ports, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid USB port pattern: %w", err)
	}
	sort.Strings(ports)
	return ports, nil
}

func hasGlobMeta(value string) bool {
	for _, current := range value {
		if current == '*' || current == '?' || current == '[' || current == '\\' {
			return true
		}
	}
	return false
}
