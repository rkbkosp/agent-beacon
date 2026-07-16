package protocol

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDeepNestedJSONDoesNotHang(t *testing.T) {
	// Valid nested object: {"a":{"a":...{"v":2}...}}
	for _, depth := range []int{50, 200, 1000, 5000} {
		depth := depth
		t.Run(fmt.Sprintf("depth_%d", depth), func(t *testing.T) {
			var b strings.Builder
			for i := 0; i < depth; i++ {
				b.WriteString(`{"a":`)
			}
			b.WriteString(`{"v":2}`)
			for i := 0; i < depth; i++ {
				b.WriteByte('}')
			}
			data := []byte(b.String())
			if len(data) > MaxMessageBytes {
				t.Skipf("size %d exceeds max", len(data))
			}
			start := time.Now()
			done := make(chan error, 1)
			go func() {
				_, err := Decode(data)
				done <- err
			}()
			select {
			case err := <-done:
				t.Logf("size=%d elapsed=%s err=%v", len(data), time.Since(start), err)
			case <-time.After(2 * time.Second):
				t.Fatalf("Decode hung/slow >2s on nested JSON size=%d depth=%d", len(data), depth)
			}
		})
	}
}

func TestDuplicateKeysJSON(t *testing.T) {
	// Many duplicate keys - encoding/json last-wins, should stay fast.
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < 3000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"id":"x%d"`, i)
	}
	b.WriteString(`,"v":2,"type":"heartbeat","ts":"2020-01-01T00:00:00Z","revision":0,"payload":{}}`)
	data := []byte(b.String())
	if len(data) > MaxMessageBytes {
		data = data[:MaxMessageBytes-1]
		// may be invalid, still should not hang
	}
	start := time.Now()
	_, err := Decode(data)
	t.Logf("size=%d elapsed=%s err=%v", len(data), time.Since(start), err)
	if time.Since(start) > time.Second {
		t.Fatal("too slow")
	}
}
