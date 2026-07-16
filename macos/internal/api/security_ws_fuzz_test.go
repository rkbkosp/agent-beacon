package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/state"
	"github.com/gorilla/websocket"
)

func TestDynamicWebSocketMalformedFramesDoNotCrash(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 16), DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	headers := http.Header{}
	headers.Set("X-Agent-Beacon-Device-ID", "fuzz-ws")
	headers.Set("X-Agent-Beacon-Token", testToken)
	headers.Set("X-Agent-Beacon-Protocol", "2")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = conn.ReadMessage()

	payloads := [][]byte{
		[]byte(`{`),
		[]byte(`[]`),
		[]byte("\x00\x01\x02"),
		[]byte(strings.Repeat("A", 70*1024)),
		[]byte(`{"type":"ack","v":2}`),
		[]byte(`{"v":2,"id":"h","type":"hello","ts":"2020-01-01T00:00:00Z","revision":0,"payload":{"role":"device","device_id":"other","protocol_version":2}}`),
	}
	for _, payload := range payloads {
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		// binary may fail; text is the production path
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			// connection close under abuse is acceptable
			return
		}
	}

	// Valid hello after garbage should still be processable on a fresh connection.
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = conn2.ReadMessage()
	hello, err := protocol.NewEnvelope("hello-ok", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "fuzz-ws", ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(hello)
	if err := conn2.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatal(err)
	}
	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn2.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.Decode(msg)
	if err != nil || envelope.Type != protocol.TypeSnapshot {
		t.Fatalf("expected snapshot after valid hello, got %s err=%v", msg, err)
	}
}
