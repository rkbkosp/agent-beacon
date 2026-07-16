package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
	"agent-beacon/internal/state"
	"github.com/gorilla/websocket"
)

const testToken = "test-bridge-token"

type channelDeviceTransport struct {
	reads     chan []byte
	writes    chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newChannelDeviceTransport() *channelDeviceTransport {
	return &channelDeviceTransport{
		reads: make(chan []byte, 8), writes: make(chan []byte, 8), closed: make(chan struct{}),
	}
}

func (*channelDeviceTransport) Name() string { return "usb:test" }

func (transport *channelDeviceTransport) ReadMessage(ctx context.Context) ([]byte, error) {
	select {
	case data := <-transport.reads:
		return data, nil
	case <-transport.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (transport *channelDeviceTransport) WriteMessage(ctx context.Context, data []byte) error {
	select {
	case transport.writes <- append([]byte(nil), data...):
		return nil
	case <-transport.closed:
		return io.ErrClosedPipe
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (transport *channelDeviceTransport) Close() error {
	transport.closeOnce.Do(func() { close(transport.closed) })
	return nil
}

func authorizedRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Beacon-Token", testToken)
	return request
}

func notificationEnvelope(t *testing.T, id, dedupeKey string, expiresAt time.Time) protocol.Envelope {
	t.Helper()
	envelope, err := protocol.NewEnvelope(id, protocol.TypeNotification, 0, time.Now().UTC(), protocol.Notification{
		Category: protocol.CategorySystem, Kind: "system.provider_error", Source: "http-test",
		SubjectID: "listener", Theme: protocol.ThemeYellow, Urgency: protocol.UrgencyNormal,
		Priority: 44, DedupeKey: dedupeKey, Title: "Listener test", Detail: "HTTP ingress",
		SourceLabel: "Test", DisplayMS: 3000, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func postNotification(t *testing.T, rawURL string, envelope protocol.Envelope) (*http.Response, NotificationReceipt) {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-Agent-Beacon-Token", testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt NotificationReceipt
	if err := json.NewDecoder(response.Body).Decode(&receipt); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	return response, receipt
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

func readTransportEnvelope(t *testing.T, transport *channelDeviceTransport) protocol.Envelope {
	t.Helper()
	select {
	case data := <-transport.writes:
		envelope, err := protocol.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		return envelope
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for device transport message")
		return protocol.Envelope{}
	}
}

func TestUSBDeviceTransportSharesHandshakeSnapshotAndBroadcastPath(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	bridge := NewServer(store, DefaultSnapshot(), testToken)
	transport := newChannelDeviceTransport()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bridge.ServeDeviceTransport(ctx, transport) }()

	serverHello := readTransportEnvelope(t, transport)
	if serverHello.Type != protocol.TypeHello {
		t.Fatalf("first USB envelope type = %s", serverHello.Type)
	}
	deviceHello, err := protocol.NewEnvelope("hello-usb-device", protocol.TypeHello, 0,
		time.Now().UTC(), protocol.Hello{Role: "device", DeviceID: "device-usb",
			AuthToken: testToken, ProtocolVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(deviceHello)
	transport.reads <- data
	snapshot := readTransportEnvelope(t, transport)
	if snapshot.Type != protocol.TypeSnapshot {
		t.Fatalf("post-handshake USB envelope type = %s", snapshot.Type)
	}
	ids := bridge.hub.deviceIDs()
	if len(ids) != 1 || ids[0] != "device-usb" {
		t.Fatalf("USB device IDs = %v", ids)
	}
	connections := bridge.hub.connections()
	if len(connections) != 1 || connections[0].Transport != "usb:test" || !connections[0].Ready {
		t.Fatalf("USB connections = %+v", connections)
	}

	notification := notificationEnvelope(t, "unused", "system:usb:broadcast", time.Now().Add(time.Minute))
	payload, err := protocol.DecodePayload[protocol.Notification](notification)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := bridge.PublishNotification(payload)
	if err != nil || receipt.Status != state.EventAccepted {
		t.Fatalf("publish receipt=%+v err=%v", receipt, err)
	}
	received := readTransportEnvelope(t, transport)
	if received.Type != protocol.TypeNotification || received.Revision != receipt.Revision {
		t.Fatalf("USB broadcast = %+v", received)
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ServeDeviceTransport returned %v", err)
	}
}

func TestUSBDeviceHelloRequiresBridgeToken(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 10), DefaultSnapshot(), testToken)
	current := &client{
		transport: "usb:test", send: make(chan []byte, 1), done: make(chan struct{}),
	}
	hello, err := protocol.NewEnvelope("hello-usb-unauthorized", protocol.TypeHello, 0,
		time.Now().UTC(), protocol.Hello{Role: "device", DeviceID: "device-usb", ProtocolVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(hello)
	bridge.processDeviceMessage(current, data)
	if current.id != "" || current.ready.Load() || len(current.send) != 0 || len(bridge.hub.connections()) != 0 {
		t.Fatal("unauthenticated USB device hello was accepted")
	}
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
		method string
		path   string
		want   int
	}{{http.MethodGet, "/healthz", http.StatusOK}, {http.MethodGet, "/readyz", http.StatusUnauthorized},
		{http.MethodGet, "/v2/snapshot", http.StatusUnauthorized}, {http.MethodPost, "/v2/notifications", http.StatusUnauthorized},
		{http.MethodGet, "/v1/snapshot", http.StatusNotFound}} {
		request, err := http.NewRequest(testCase.method, server.URL+testCase.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != testCase.want {
			t.Fatalf("GET %s = %d, want %d", testCase.path, response.StatusCode, testCase.want)
		}
	}
}

func TestHTTPNotificationIsAcceptedStoredAndBroadcast(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	bridge := NewServer(store, DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()

	connection := dialDevice(t, server.URL)
	defer connection.Close()
	_ = readEnvelope(t, connection)
	hello, err := protocol.NewEnvelope("device-hello-http", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "device-test", ProtocolVersion: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	_ = readEnvelope(t, connection)

	submitted := notificationEnvelope(t, "evt-http-1", "system:http-listener:1", time.Now().Add(time.Minute))
	response, receipt := postNotification(t, server.URL+"/v2/notifications", submitted)
	if response.StatusCode != http.StatusAccepted || receipt.Status != state.EventAccepted ||
		receipt.EventID != submitted.ID || receipt.Revision != 1 {
		t.Fatalf("POST notification status=%d receipt=%+v", response.StatusCode, receipt)
	}
	received := readEnvelope(t, connection)
	if received.Type != protocol.TypeNotification || received.ID != submitted.ID || received.Revision != receipt.Revision {
		t.Fatalf("broadcast envelope = %+v", received)
	}
	events := store.Events(10)
	if len(events) != 1 || events[0].ID != submitted.ID || events[0].Revision != receipt.Revision {
		t.Fatalf("stored events = %+v", events)
	}
}

func TestPublishNotificationUsesTheSameValidationAndDedupePath(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken)
	envelope := notificationEnvelope(t, "unused", "system:in-process:dedupe", time.Now().Add(time.Minute))
	notification, err := protocol.DecodePayload[protocol.Notification](envelope)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := bridge.PublishNotification(notification)
	if err != nil || receipt.Status != state.EventAccepted || receipt.EventID == "" || receipt.Revision != 1 {
		t.Fatalf("first publish receipt=%+v err=%v", receipt, err)
	}
	receipt, err = bridge.PublishNotification(notification)
	if err != nil || receipt.Status != state.EventDuplicate || receipt.Revision != 0 {
		t.Fatalf("duplicate publish receipt=%+v err=%v", receipt, err)
	}
	notification.Title = ""
	if _, err := bridge.PublishNotification(notification); err == nil {
		t.Fatal("invalid in-process notification was accepted")
	}
}

func TestHTTPNotificationReportsDuplicateAndExpired(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()

	first := notificationEnvelope(t, "evt-http-first", "system:http-listener:dedupe", time.Now().Add(time.Minute))
	response, receipt := postNotification(t, server.URL+"/v2/notifications", first)
	if response.StatusCode != http.StatusAccepted || receipt.Status != state.EventAccepted {
		t.Fatalf("first POST status=%d receipt=%+v", response.StatusCode, receipt)
	}
	duplicate := notificationEnvelope(t, "evt-http-second", "system:http-listener:dedupe", time.Now().Add(time.Minute))
	response, receipt = postNotification(t, server.URL+"/v2/notifications", duplicate)
	if response.StatusCode != http.StatusOK || receipt.Status != state.EventDuplicate || receipt.Revision != 0 {
		t.Fatalf("duplicate POST status=%d receipt=%+v", response.StatusCode, receipt)
	}
	expired := notificationEnvelope(t, "evt-http-expired", "system:http-listener:expired", time.Now().Add(-time.Second))
	response, receipt = postNotification(t, server.URL+"/v2/notifications", expired)
	if response.StatusCode != http.StatusGone || receipt.Status != state.EventExpired || receipt.Revision != 0 {
		t.Fatalf("expired POST status=%d receipt=%+v", response.StatusCode, receipt)
	}
}

func TestHTTPNotificationRejectsInvalidRequests(t *testing.T) {
	bridge := NewServerWithLimits(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken, 8, 128)
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()

	testCases := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{name: "media type", contentType: "text/plain", body: `{}`, want: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", contentType: "application/json", body: `{`, want: http.StatusBadRequest},
		{name: "oversized", contentType: "application/json", body: strings.Repeat("x", 129), want: http.StatusRequestEntityTooLarge},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v2/notifications", strings.NewReader(testCase.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Agent-Beacon-Token", testToken)
			request.Header.Set("Content-Type", testCase.contentType)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != testCase.want {
				t.Fatalf("status = %d, want %d", response.StatusCode, testCase.want)
			}
		})
	}

	validationServer := httptest.NewServer(NewServer(state.NewStore(time.Minute, 100), DefaultSnapshot(), testToken).Handler())
	defer validationServer.Close()
	nonzeroRevision := notificationEnvelope(t, "evt-http-revision", "system:http-listener:revision", time.Now().Add(time.Minute))
	nonzeroRevision.Revision = 12
	response, _ := postNotification(t, validationServer.URL+"/v2/notifications", nonzeroRevision)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("nonzero revision status = %d", response.StatusCode)
	}
	notificationType := nonzeroRevision
	notificationType.Revision = 0
	notificationType.Type = protocol.TypeHeartbeat
	response, _ = postNotification(t, validationServer.URL+"/v2/notifications", notificationType)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("non-notification type status = %d", response.StatusCode)
	}
	data, err := json.Marshal(notificationEnvelope(t, "evt-http-unknown", "system:http-listener:unknown", time.Now().Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"payload":{`), []byte(`"payload":{"unexpected":true,`), 1)
	request, err := http.NewRequest(http.MethodPost, validationServer.URL+"/v2/notifications", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Beacon-Token", testToken)
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown payload field status = %d", response.StatusCode)
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
