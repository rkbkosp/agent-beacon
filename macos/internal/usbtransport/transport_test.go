package usbtransport

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type memoryPort struct {
	mu      sync.Mutex
	reads   [][]byte
	written bytes.Buffer
	closed  bool
}

type timeoutEOFPort struct {
	*memoryPort
	timeouts int
}

func (port *timeoutEOFPort) Read(output []byte) (int, error) {
	if port.timeouts > 0 {
		port.timeouts--
		return 0, io.EOF
	}
	return port.memoryPort.Read(output)
}

func (port *memoryPort) Read(output []byte) (int, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.reads) > 0 {
		chunk := port.reads[0]
		port.reads = port.reads[1:]
		return copy(output, chunk), nil
	}
	if port.closed {
		return 0, io.EOF
	}
	return 0, nil
}

func (port *memoryPort) Write(data []byte) (int, error) {
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.closed {
		return 0, io.ErrClosedPipe
	}
	return port.written.Write(data)
}

func (port *memoryPort) Close() error {
	port.mu.Lock()
	port.closed = true
	port.mu.Unlock()
	return nil
}

func TestFramedPortReadsFragmentedMessagesAndWritesFrames(t *testing.T) {
	first, _ := Encode([]byte("first"))
	second, _ := Encode([]byte("second"))
	wire := append(first, second...)
	port := &memoryPort{reads: [][]byte{wire[:2], wire[2:9], wire[9:]}}
	transport := NewFramedPort("test", port)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, want := range []string{"first", "second"} {
		message, err := transport.ReadMessage(ctx)
		if err != nil || string(message) != want {
			t.Fatalf("message=%q err=%v, want %q", message, err, want)
		}
	}
	if err := transport.WriteMessage(ctx, []byte("reply")); err != nil {
		t.Fatal(err)
	}
	var decoder Decoder
	frames, rejected := decoder.Feed(port.written.Bytes())
	if rejected != 0 || len(frames) != 1 || string(frames[0]) != "reply" {
		t.Fatalf("written frames=%q rejected=%d", frames, rejected)
	}
}

func TestFramedPortTreatsZeroByteEOFAsTTYTimeout(t *testing.T) {
	wire, _ := Encode([]byte("after-timeout"))
	port := &timeoutEOFPort{memoryPort: &memoryPort{reads: [][]byte{wire}}, timeouts: 2}
	transport := NewFramedPort("test", port)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	message, err := transport.ReadMessage(ctx)
	if err != nil || string(message) != "after-timeout" {
		t.Fatalf("message=%q err=%v", message, err)
	}
}

func TestMatchingPortsSortsGlobResults(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"cu.usbmodem2", "cu.usbmodem1"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ports, err := matchingPorts(filepath.Join(directory, "cu.usbmodem*"))
	if err != nil || len(ports) != 2 || filepath.Base(ports[0]) != "cu.usbmodem1" ||
		filepath.Base(ports[1]) != "cu.usbmodem2" {
		t.Fatalf("ports=%v err=%v", ports, err)
	}
}
