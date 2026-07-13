package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
	"agent-beacon/internal/state"
	"github.com/gorilla/websocket"
)

const testToken = "test-bridge-token"

func authorizedRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Beacon-Token", testToken)
	return request
}

func readEnvelope(t *testing.T, connection *websocket.Conn) protocol.Envelope {
	t.Helper()
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := protocol.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func dialDevice(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	header.Set("X-Agent-Beacon-Device-ID", "device-test")
	header.Set("X-Agent-Beacon-Token", testToken)
	header.Set("X-Agent-Beacon-Protocol", "2")
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/v2/ws"
	connection, response, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		if response != nil {
			t.Fatalf("dial status=%d: %v", response.StatusCode, err)
		}
		t.Fatal(err)
	}
	return connection
}

func TestHealthIsPublicAndOtherHTTPRoutesRequireToken(t *testing.T) {
	server := httptest.NewServer(NewServer(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken).Handler())
	defer server.Close()
	for _, testCase := range []struct {
		path string
		want int
	}{{"/healthz", http.StatusOK}, {"/readyz", http.StatusUnauthorized}, {"/v2/snapshot", http.StatusUnauthorized}, {"/v1/snapshot", http.StatusNotFound}} {
		response, err := http.Get(server.URL + testCase.path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != testCase.want {
			t.Fatalf("GET %s = %d, want %d", testCase.path, response.StatusCode, testCase.want)
		}
	}
}

func TestWebSocketHandshakeFixtureAndACKRoundTrip(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	server := httptest.NewServer(NewServer(store, DefaultSnapshot(), testToken).Handler())
	defer server.Close()

	connection := dialDevice(t, server.URL)
	defer connection.Close()
	if got := readEnvelope(t, connection); got.Type != protocol.TypeHello {
		t.Fatalf("first message = %q", got.Type)
	}
	hello, err := protocol.NewEnvelope("device-hello-1", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "device-test", ProtocolVersion: 2, FirmwareVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, connection); got.Type != protocol.TypeSnapshot {
		t.Fatalf("second message = %q", got.Type)
	}

	request := authorizedRequest(t, http.MethodPost, server.URL+"/v2/fixtures/herdr-blocked")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("fixture status = %d", response.StatusCode)
	}
	notification := readEnvelope(t, connection)
	if notification.Type != protocol.TypeStatePatch {
		t.Fatalf("fixture first message = %q, want state_patch", notification.Type)
	}
	notification = readEnvelope(t, connection)
	if notification.Type != protocol.TypeNotification {
		t.Fatalf("fixture second message = %q, want notification", notification.Type)
	}

	ack := protocol.ACK{V: 2, Type: protocol.TypeACK, ID: notification.ID, DeviceID: "device-test", Status: protocol.ACKShown, At: time.Now().UTC()}
	if err := connection.WriteJSON(ack); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(store.ACKs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if acks := store.ACKs(); len(acks) != 1 || acks[0].ACK.ID != notification.ID {
		t.Fatalf("ACKs = %+v", acks)
	}
}

func TestWebSocketRejectsMissingHeaders(t *testing.T) {
	server := httptest.NewServer(NewServer(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken).Handler())
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/ws"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial err=%v response=%v", err, response)
	}
}

func TestFixtureUpdatesSnapshotAndEventsLimit(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	server := httptest.NewServer(NewServer(store, DefaultSnapshot(), testToken).Handler())
	defer server.Close()
	for _, name := range []string{"herdr-blocked", "herdr-done"} {
		request := authorizedRequest(t, http.MethodPost, server.URL+"/v2/fixtures/"+url.PathEscape(name))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("fixture %s = %d", name, response.StatusCode)
		}
	}
	request := authorizedRequest(t, http.MethodGet, server.URL+"/v2/events?limit=1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Events []protocol.Envelope `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("events = %d", len(payload.Events))
	}
}

func TestPublishProviderUpdateBroadcastsPatchAndUpdatesSnapshot(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	bridge := NewServer(store, DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	connection := dialDevice(t, server.URL)
	defer connection.Close()
	_ = readEnvelope(t, connection)
	hello, err := protocol.NewEnvelope("device-hello-provider", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "device-test", ProtocolVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	_ = readEnvelope(t, connection)

	agents := protocol.AgentsState{Provider: "herdr", Connected: true, UpdatedAt: time.Now(), Items: []protocol.AgentItem{
		{PaneID: "w1:p1", DisplayName: "agent-bacon", Status: protocol.AgentWorking, Revision: 1},
	}}
	if err := bridge.PublishProviderUpdate(providers.Update{Patch: protocol.StatePatch{Agents: &agents}}); err != nil {
		t.Fatal(err)
	}
	patch := readEnvelope(t, connection)
	if patch.Type != protocol.TypeStatePatch || patch.Revision != 1 {
		t.Fatalf("provider envelope = %+v", patch)
	}
	payload, err := protocol.DecodePayload[protocol.StatePatch](patch)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Agents == nil || payload.Agents.Items[0].DisplayName != "agent-bacon" {
		t.Fatalf("provider patch = %+v", payload)
	}
}

func TestProviderUpdatesRecomputeOverallFreshness(t *testing.T) {
	now := time.Now()
	snapshot := DefaultSnapshot()
	for index := range snapshot.Codex.Homes {
		snapshot.Codex.Homes[index].Freshness = protocol.FreshnessFresh
	}
	snapshot.Codex.Relay.Freshness = protocol.FreshnessFresh
	snapshot.Agents.Connected = true
	snapshot.Weather.Current.Freshness = protocol.FreshnessFresh
	snapshot.Weather.Lunch.IsPast = true
	snapshot.Weather.Leave.IsPast = false
	snapshot.Weather.Leave.Freshness = protocol.FreshnessFresh
	snapshot.System.OverallFreshness = protocol.FreshnessUnknown
	server := NewServer(state.NewStore(time.Minute, 16), snapshot, "token")
	clock := protocol.ClockState{Timezone: "Asia/Shanghai", ServerTime: now}
	if err := server.PublishProviderUpdate(providers.Update{Patch: protocol.StatePatch{Clock: &clock}}); err != nil {
		t.Fatal(err)
	}
	server.snapshotMu.RLock()
	got := server.snapshot.System.OverallFreshness
	server.snapshotMu.RUnlock()
	if got != protocol.FreshnessFresh {
		t.Fatalf("overall freshness = %q", got)
	}
}

func TestProviderBroadcastWaitsForDeviceHello(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	bridge := NewServer(store, DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	connection := dialDevice(t, server.URL)
	defer connection.Close()
	if got := readEnvelope(t, connection); got.Type != protocol.TypeHello {
		t.Fatalf("first message = %q, want hello", got.Type)
	}

	agents := protocol.AgentsState{Provider: "herdr", Connected: true, UpdatedAt: time.Now(), Items: []protocol.AgentItem{
		{PaneID: "w1:p1", DisplayName: "agent-bacon", Status: protocol.AgentWorking, Revision: 1},
	}}
	if err := bridge.PublishProviderUpdate(providers.Update{Patch: protocol.StatePatch{Agents: &agents}}); err != nil {
		t.Fatal(err)
	}

	hello, err := protocol.NewEnvelope("device-hello-after-provider", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "device-test", ProtocolVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	if got := readEnvelope(t, connection); got.Type != protocol.TypeSnapshot {
		t.Fatalf("message after device hello = %q, want snapshot", got.Type)
	}
}
