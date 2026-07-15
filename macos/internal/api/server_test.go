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

func TestSnapshotEnvelopeNormalizesTimesToClockTimezone(t *testing.T) {
	weeklyReset := time.Date(2026, time.July, 14, 16, 30, 0, 0, time.UTC)
	cardExpiry := time.Date(2026, time.July, 14, 23, 59, 0, 0, time.UTC)
	snapshot := DefaultSnapshot()
	snapshot.Clock.Timezone = "Asia/Shanghai"
	snapshot.Codex.Homes[0].WeeklyResetAt = &weeklyReset
	snapshot.Codex.Homes[0].NearestResetCardExpiresAt = &cardExpiry
	snapshot.Weather.Current.ObservedAt = weeklyReset

	bridge := NewServer(state.NewStore(time.Minute, 100), snapshot, testToken)
	envelope := bridge.snapshotEnvelope()
	if raw := string(envelope.Payload); !strings.Contains(raw, `"weekly_reset_at":"2026-07-15T00:30:00+08:00"`) ||
		!strings.Contains(raw, `"nearest_reset_card_expires_at":"2026-07-15T07:59:00+08:00"`) {
		t.Fatalf("snapshot payload was not timezone-normalized: %s", raw)
	}
	payload, err := protocol.DecodePayload[protocol.Snapshot](envelope)
	if err != nil {
		t.Fatal(err)
	}
	if got := payload.Codex.Homes[0].WeeklyResetAt.Format(time.RFC3339); got != "2026-07-15T00:30:00+08:00" {
		t.Fatalf("weekly reset = %s", got)
	}
	if got := payload.Codex.Homes[0].NearestResetCardExpiresAt.Format(time.RFC3339); got != "2026-07-15T07:59:00+08:00" {
		t.Fatalf("card expiry = %s", got)
	}
	if got := payload.Weather.Current.ObservedAt.Format(time.RFC3339); got != "2026-07-15T00:30:00+08:00" {
		t.Fatalf("weather observed_at = %s", got)
	}
	_, clockOffset := payload.Clock.ServerTime.Zone()
	if clockOffset != 8*60*60 {
		t.Fatalf("clock offset = %d", clockOffset)
	}
	if got := snapshot.Codex.Homes[0].WeeklyResetAt.Format(time.RFC3339); got != "2026-07-14T16:30:00Z" {
		t.Fatalf("source snapshot was mutated: %s", got)
	}
}

func TestPublishProviderUpdateNormalizesCodexAndWeatherTimes(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	bridge := NewServer(store, DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	connection := dialDevice(t, server.URL)
	defer connection.Close()
	_ = readEnvelope(t, connection)
	hello, err := protocol.NewEnvelope("device-hello-timezone", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "device-test", ProtocolVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	_ = readEnvelope(t, connection)

	base := DefaultSnapshot()
	weeklyReset := time.Date(2026, time.July, 14, 16, 30, 0, 0, time.UTC)
	cardExpiry := time.Date(2026, time.July, 14, 23, 59, 0, 0, time.UTC)
	base.Codex.Homes[0].WeeklyResetAt = &weeklyReset
	base.Codex.Homes[0].NearestResetCardExpiresAt = &cardExpiry
	base.Weather.Current.ObservedAt = weeklyReset
	base.Weather.Lunch.TargetAt = cardExpiry
	base.Weather.NextOuting.TargetAt = cardExpiry
	if err := bridge.PublishProviderUpdate(providers.Update{Patch: protocol.StatePatch{Codex: &base.Codex}}); err != nil {
		t.Fatal(err)
	}

	codexEnvelope := readEnvelope(t, connection)
	if raw := string(codexEnvelope.Payload); !strings.Contains(raw, `"weekly_reset_at":"2026-07-15T00:30:00+08:00"`) ||
		!strings.Contains(raw, `"nearest_reset_card_expires_at":"2026-07-15T07:59:00+08:00"`) {
		t.Fatalf("Codex patch payload was not timezone-normalized: %s", raw)
	}
	codexPatch, err := protocol.DecodePayload[protocol.StatePatch](codexEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if codexPatch.Clock != nil || codexPatch.Codex == nil || codexPatch.Weather != nil {
		t.Fatalf("Codex-only provider patch domains = %+v", codexPatch)
	}
	if got := codexPatch.Codex.Homes[0].WeeklyResetAt.Format(time.RFC3339); got != "2026-07-15T00:30:00+08:00" {
		t.Fatalf("weekly reset = %s", got)
	}
	if got := codexPatch.Codex.Homes[0].NearestResetCardExpiresAt.Format(time.RFC3339); got != "2026-07-15T07:59:00+08:00" {
		t.Fatalf("card expiry = %s", got)
	}

	if err := bridge.PublishProviderUpdate(providers.Update{Patch: protocol.StatePatch{Weather: &base.Weather}}); err != nil {
		t.Fatal(err)
	}
	weatherEnvelope := readEnvelope(t, connection)
	if raw := string(weatherEnvelope.Payload); !strings.Contains(raw, `"observed_at":"2026-07-15T00:30:00+08:00"`) ||
		!strings.Contains(raw, `"target_at":"2026-07-15T07:59:00+08:00"`) {
		t.Fatalf("weather patch payload was not timezone-normalized: %s", raw)
	}
	weatherPatch, err := protocol.DecodePayload[protocol.StatePatch](weatherEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if weatherPatch.Clock != nil || weatherPatch.Codex != nil || weatherPatch.Weather == nil {
		t.Fatalf("weather-only provider patch domains = %+v", weatherPatch)
	}
	if got := weatherPatch.Weather.Current.ObservedAt.Format(time.RFC3339); got != "2026-07-15T00:30:00+08:00" {
		t.Fatalf("weather observed_at = %s", got)
	}
	if got := weatherPatch.Weather.Lunch.TargetAt.Format(time.RFC3339); got != "2026-07-15T07:59:00+08:00" {
		t.Fatalf("weather lunch target_at = %s", got)
	}
	if got := weatherPatch.Weather.NextOuting.TargetAt.Format(time.RFC3339); got != "2026-07-15T07:59:00+08:00" {
		t.Fatalf("next outing target_at = %s", got)
	}
	if got := base.Codex.Homes[0].WeeklyResetAt.Format(time.RFC3339); got != "2026-07-14T16:30:00Z" {
		t.Fatalf("provider state was mutated: %s", got)
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
