package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
	"agent-beacon/internal/providers/mock"
	"agent-beacon/internal/state"
	"github.com/gorilla/websocket"
)

const (
	defaultDeviceSendQueue = 64
	defaultMaxRequestBytes = 256 * 1024
)

type client struct {
	id             string
	transport      string
	closeTransport func() error
	send           chan []byte
	done           chan struct{}
	closeOnce      sync.Once
	ready          atomic.Bool
}

func (client *client) close() {
	client.closeOnce.Do(func() {
		close(client.done)
		if client.closeTransport != nil {
			_ = client.closeTransport()
		}
	})
}

type DeviceTransport interface {
	Name() string
	ReadMessage(context.Context) ([]byte, error)
	WriteMessage(context.Context, []byte) error
	Close() error
}

type hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

type DeviceConnection struct {
	DeviceID  string `json:"device_id"`
	Transport string `json:"transport"`
	Ready     bool   `json:"ready"`
}

func newHub() *hub { return &hub{clients: make(map[*client]struct{})} }

func (hub *hub) add(client *client) {
	hub.mu.Lock()
	hub.clients[client] = struct{}{}
	hub.mu.Unlock()
}

func (hub *hub) remove(client *client) {
	hub.mu.Lock()
	delete(hub.clients, client)
	hub.mu.Unlock()
	client.close()
}

func (hub *hub) broadcast(data []byte) {
	hub.mu.RLock()
	clients := make([]*client, 0, len(hub.clients))
	for current := range hub.clients {
		clients = append(clients, current)
	}
	hub.mu.RUnlock()
	for _, current := range clients {
		if !current.ready.Load() {
			continue
		}
		select {
		case current.send <- data:
		default:
			hub.remove(current)
		}
	}
}

func (hub *hub) deviceIDs() []string {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	unique := make(map[string]struct{}, len(hub.clients))
	for current := range hub.clients {
		if current.id != "" {
			unique[current.id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (hub *hub) connections() []DeviceConnection {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	connections := make([]DeviceConnection, 0, len(hub.clients))
	for current := range hub.clients {
		if current.id != "" {
			connections = append(connections, DeviceConnection{
				DeviceID: current.id, Transport: current.transport, Ready: current.ready.Load(),
			})
		}
	}
	sort.Slice(connections, func(left, right int) bool {
		if connections[left].DeviceID != connections[right].DeviceID {
			return connections[left].DeviceID < connections[right].DeviceID
		}
		return connections[left].Transport < connections[right].Transport
	})
	return connections
}

type Server struct {
	store           *state.Store
	snapshotMu      sync.RWMutex
	snapshot        protocol.Snapshot
	token           string
	hub             *hub
	upgrader        websocket.Upgrader
	sequence        atomic.Uint64
	sendQueue       int
	maxRequestBytes int64
	fixturesEnabled atomic.Bool
}

type NotificationReceipt struct {
	Status   state.EventResult `json:"status"`
	EventID  string            `json:"event_id"`
	Revision uint64            `json:"revision,omitempty"`
}

func NewServer(store *state.Store, snapshot protocol.Snapshot, token string) *Server {
	return NewServerWithLimits(store, snapshot, token, defaultDeviceSendQueue, defaultMaxRequestBytes)
}

func NewServerWithLimits(store *state.Store, snapshot protocol.Snapshot, token string, sendQueue int, maxRequestBytes int64) *Server {
	if sendQueue < 1 {
		sendQueue = defaultDeviceSendQueue
	}
	if maxRequestBytes < 1 {
		maxRequestBytes = defaultMaxRequestBytes
	}
	server := &Server{
		store: store, snapshot: snapshot, token: token, hub: newHub(), sendQueue: sendQueue,
		maxRequestBytes: maxRequestBytes,
		upgrader:        websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
	server.fixturesEnabled.Store(true)
	return server
}

func (server *Server) SetFixturesEnabled(enabled bool) { server.fixturesEnabled.Store(enabled) }

func DefaultSnapshot() protocol.Snapshot { return mock.DefaultSnapshot(time.Now()) }

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /readyz", server.auth(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	})))
	mux.Handle("GET /v2/snapshot", server.auth(http.HandlerFunc(server.handleSnapshot)))
	mux.Handle("GET /v2/events", server.auth(http.HandlerFunc(server.handleEvents)))
	mux.Handle("GET /v2/devices", server.auth(http.HandlerFunc(server.handleDevices)))
	mux.Handle("POST /v2/notifications", server.auth(http.HandlerFunc(server.handleNotification)))
	mux.Handle("POST /v2/fixtures/{name}", server.auth(http.HandlerFunc(server.handleFixture)))
	mux.HandleFunc("GET /v2/ws", server.handleWebSocket)
	return mux
}

func (server *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !server.validToken(request.Header.Get("X-Agent-Beacon-Token")) {
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) validToken(candidate string) bool {
	if server.token == "" || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(server.token)) == 1
}

func (server *Server) nextID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), server.sequence.Add(1))
}

func (server *Server) snapshotEnvelope() protocol.Envelope {
	server.snapshotMu.RLock()
	snapshot := server.snapshot
	server.snapshotMu.RUnlock()
	snapshot.Clock.ServerTime = time.Now()
	snapshot = normalizeSnapshotTimes(snapshot)
	envelope, _ := protocol.NewEnvelope(server.nextID("snapshot"), protocol.TypeSnapshot, server.store.Revision(), time.Now().UTC(), snapshot)
	return envelope
}

func (server *Server) helloEnvelope() protocol.Envelope {
	envelope, _ := protocol.NewEnvelope(server.nextID("hello"), protocol.TypeHello, server.store.Revision(), time.Now().UTC(), protocol.Hello{
		Role: "server", ProtocolVersion: protocol.Version, BridgeVersion: "bridge-v2",
	})
	return envelope
}

func (server *Server) heartbeatEnvelope() protocol.Envelope {
	envelope, _ := protocol.NewEnvelope(server.nextID("heartbeat"), protocol.TypeHeartbeat,
		server.store.Revision(), time.Now().UTC(), protocol.Heartbeat{})
	return envelope
}

func (server *Server) handleSnapshot(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, server.snapshotEnvelope())
}

func (server *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 1000"})
			return
		}
		limit = parsed
	}
	writeJSON(writer, http.StatusOK, map[string]any{"events": server.store.Events(limit), "acks": server.store.ACKs()})
}

func (server *Server) handleDevices(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"devices": server.hub.deviceIDs(), "connections": server.hub.connections(),
	})
}

func (server *Server) handleNotification(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, server.maxRequestBytes)
	data, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large"})
			return
		}
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "read request body"})
		return
	}
	envelope, err := protocol.Decode(data)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if envelope.Type != protocol.TypeNotification {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "envelope type must be notification"})
		return
	}
	if envelope.Revision != 0 {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "notification revision must be 0"})
		return
	}

	receipt := server.publishNotificationEnvelope(envelope, time.Now().UTC())
	status := http.StatusAccepted
	switch receipt.Status {
	case state.EventDuplicate:
		status = http.StatusOK
	case state.EventExpired:
		status = http.StatusGone
	}
	writeJSON(writer, status, receipt)
}

func (server *Server) handleFixture(writer http.ResponseWriter, request *http.Request) {
	if !server.fixturesEnabled.Load() {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "fixtures are disabled"})
		return
	}
	fixture, err := mock.Build(request.PathValue("name"), time.Now())
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	patch, _ := server.applyPatch(fixture.Patch)
	patchRevision := server.store.NextRevision()
	patchEnvelope, err := protocol.NewEnvelope(server.nextID("patch"), protocol.TypeStatePatch, patchRevision, time.Now().UTC(), patch)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "invalid fixture patch"})
		return
	}
	server.broadcast(patchEnvelope)
	response := map[string]any{"status": "accepted", "fixture": request.PathValue("name"), "revision": patchRevision}
	if fixture.Notification != nil {
		receipt, publishError := server.PublishNotification(*fixture.Notification)
		if publishError != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "invalid fixture notification"})
			return
		}
		response["event_status"] = receipt.Status
		if receipt.Status == state.EventAccepted {
			response["event_id"] = receipt.EventID
		}
	}
	writeJSON(writer, http.StatusAccepted, response)
}

func (server *Server) PublishNotification(notification protocol.Notification) (NotificationReceipt, error) {
	now := time.Now().UTC()
	envelope, err := protocol.NewEnvelope(server.nextID("evt"), protocol.TypeNotification, 0, now, notification)
	if err != nil {
		return NotificationReceipt{}, fmt.Errorf("encode notification: %w", err)
	}
	return server.publishNotificationEnvelope(envelope, now), nil
}

func (server *Server) publishNotificationEnvelope(envelope protocol.Envelope, now time.Time) NotificationReceipt {
	result, accepted := server.store.AcceptEvent(envelope, now)
	receipt := NotificationReceipt{Status: result, EventID: envelope.ID}
	if result == state.EventAccepted {
		receipt.Revision = accepted.Revision
		server.broadcast(accepted)
	}
	return receipt
}

func (server *Server) PublishProviderUpdate(update providers.Update) error {
	if update.Patch.Clock != nil || update.Patch.Codex != nil || update.Patch.Agents != nil ||
		update.Patch.Weather != nil || update.Patch.System != nil {
		patch, system := server.applyPatch(update.Patch)
		if patch.System == nil {
			patch.System = &system
		}
		revision := server.store.NextRevision()
		envelope, err := protocol.NewEnvelope(server.nextID("patch"), protocol.TypeStatePatch,
			revision, time.Now().UTC(), patch)
		if err != nil {
			return fmt.Errorf("encode provider patch: %w", err)
		}
		server.broadcast(envelope)
	}
	if update.Notification != nil {
		if _, err := server.PublishNotification(*update.Notification); err != nil {
			return fmt.Errorf("publish provider notification: %w", err)
		}
	}
	return nil
}

func (server *Server) applyPatch(patch protocol.StatePatch) (protocol.StatePatch, protocol.SystemState) {
	server.snapshotMu.Lock()
	defer server.snapshotMu.Unlock()
	timezone := server.snapshot.Clock.Timezone
	if patch.Clock != nil && patch.Clock.Timezone != "" {
		timezone = patch.Clock.Timezone
	}
	patch = normalizeStatePatchTimes(patch, timezone)
	if patch.Clock != nil {
		server.snapshot.Clock = *patch.Clock
	}
	if patch.Codex != nil {
		server.snapshot.Codex = *patch.Codex
	}
	if patch.Agents != nil {
		server.snapshot.Agents = *patch.Agents
	}
	if patch.Weather != nil {
		server.snapshot.Weather = *patch.Weather
	}
	if patch.System != nil {
		server.snapshot.System = *patch.System
	} else {
		server.snapshot.System.BridgeOnline = true
		server.snapshot.System.OverallFreshness = overallFreshness(server.snapshot)
	}
	return patch, server.snapshot.System
}

func normalizeSnapshotTimes(snapshot protocol.Snapshot) protocol.Snapshot {
	location, err := time.LoadLocation(snapshot.Clock.Timezone)
	if err != nil {
		return snapshot
	}
	snapshot.Clock.ServerTime = inLocation(snapshot.Clock.ServerTime, location)
	snapshot.Codex = normalizeCodexTimes(snapshot.Codex, location)
	snapshot.Agents.UpdatedAt = inLocation(snapshot.Agents.UpdatedAt, location)
	snapshot.Weather = normalizeWeatherTimes(snapshot.Weather, location)
	return snapshot
}

func normalizeStatePatchTimes(patch protocol.StatePatch, timezone string) protocol.StatePatch {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return patch
	}
	if patch.Clock != nil {
		clock := *patch.Clock
		clock.ServerTime = inLocation(clock.ServerTime, location)
		patch.Clock = &clock
	}
	if patch.Codex != nil {
		codex := normalizeCodexTimes(*patch.Codex, location)
		patch.Codex = &codex
	}
	if patch.Agents != nil {
		agents := *patch.Agents
		agents.UpdatedAt = inLocation(agents.UpdatedAt, location)
		patch.Agents = &agents
	}
	if patch.Weather != nil {
		weather := normalizeWeatherTimes(*patch.Weather, location)
		patch.Weather = &weather
	}
	return patch
}

func normalizeCodexTimes(codex protocol.CodexState, location *time.Location) protocol.CodexState {
	homes := append([]protocol.CodexHome(nil), codex.Homes...)
	for index := range homes {
		homes[index].WeeklyResetAt = timePointerInLocation(homes[index].WeeklyResetAt, location)
		homes[index].NearestResetCardExpiresAt = timePointerInLocation(homes[index].NearestResetCardExpiresAt, location)
		homes[index].UpdatedAt = inLocation(homes[index].UpdatedAt, location)
	}
	codex.Homes = homes
	codex.Relay.UpdatedAt = inLocation(codex.Relay.UpdatedAt, location)
	codex.TokenRate.UpdatedAt = timePointerInLocation(codex.TokenRate.UpdatedAt, location)
	return codex
}

func normalizeWeatherTimes(weather protocol.WeatherState, location *time.Location) protocol.WeatherState {
	weather.Current.ObservedAt = inLocation(weather.Current.ObservedAt, location)
	weather.Lunch.TargetAt = inLocation(weather.Lunch.TargetAt, location)
	weather.Leave.TargetAt = inLocation(weather.Leave.TargetAt, location)
	weather.NextOuting.TargetAt = inLocation(weather.NextOuting.TargetAt, location)
	weather.UpdatedAt = inLocation(weather.UpdatedAt, location)
	return weather
}

func timePointerInLocation(value *time.Time, location *time.Location) *time.Time {
	if value == nil {
		return nil
	}
	converted := inLocation(*value, location)
	return &converted
}

func inLocation(value time.Time, location *time.Location) time.Time {
	if value.IsZero() {
		return value
	}
	return value.In(location)
}

func overallFreshness(snapshot protocol.Snapshot) protocol.Freshness {
	values := make([]protocol.Freshness, 0, len(snapshot.Codex.Homes)+4)
	for _, home := range snapshot.Codex.Homes {
		values = append(values, home.Freshness)
	}
	values = append(values, snapshot.Codex.Relay.Freshness, snapshot.Weather.Current.Freshness)
	if snapshot.Codex.TokenRate.Freshness != protocol.FreshnessUnknown {
		values = append(values, snapshot.Codex.TokenRate.Freshness)
	}
	if !snapshot.Agents.Connected {
		values = append(values, protocol.FreshnessStale)
	}
	if !snapshot.Weather.Lunch.IsPast {
		values = append(values, snapshot.Weather.Lunch.Freshness)
	}
	if !snapshot.Weather.Leave.IsPast {
		values = append(values, snapshot.Weather.Leave.Freshness)
	}
	result := protocol.FreshnessFresh
	rank := map[protocol.Freshness]int{
		protocol.FreshnessFresh: 0, protocol.FreshnessCached: 1,
		protocol.FreshnessUnknown: 2, protocol.FreshnessStale: 3,
	}
	for _, value := range values {
		if rank[value] > rank[result] {
			result = value
		}
	}
	return result
}

func (server *Server) broadcast(envelope protocol.Envelope) {
	data, err := json.Marshal(envelope)
	if err == nil {
		server.hub.broadcast(data)
	}
}

func (server *Server) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	deviceID := request.Header.Get("X-Agent-Beacon-Device-ID")
	if deviceID == "" || request.Header.Get("X-Agent-Beacon-Protocol") != "2" ||
		!server.validToken(request.Header.Get("X-Agent-Beacon-Token")) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid device credentials"})
		return
	}
	connection, err := server.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	current := &client{
		id: deviceID, transport: "wifi", closeTransport: connection.Close,
		send: make(chan []byte, server.sendQueue), done: make(chan struct{}),
	}
	server.hub.add(current)
	server.enqueue(current, server.helloEnvelope())
	go server.writeWebSocket(current, connection)
	go server.readWebSocket(current, connection)
}

func (server *Server) enqueue(current *client, envelope protocol.Envelope) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	select {
	case current.send <- data:
	default:
		server.hub.remove(current)
	}
}

func (server *Server) processDeviceMessage(current *client, data []byte) {
	message, err := protocol.DecodeDeviceMessage(data)
	if err != nil {
		return
	}
	if message.ACK != nil {
		if current.ready.Load() && message.ACK.DeviceID == current.id {
			server.store.RecordACK(*message.ACK, time.Now().UTC())
		}
		return
	}
	switch message.Envelope.Type {
	case protocol.TypeHello:
		hello, decodeError := protocol.DecodePayload[protocol.Hello](*message.Envelope)
		if decodeError != nil || hello.Role != "device" || hello.DeviceID == "" ||
			(current.id != "" && hello.DeviceID != current.id) ||
			(current.transport != "wifi" && !server.validToken(hello.AuthToken)) {
			return
		}
		if current.id == "" {
			current.id = hello.DeviceID
			server.hub.add(current)
		}
		if !current.ready.Load() {
			server.enqueue(current, server.snapshotEnvelope())
			current.ready.Store(true)
		}
	case protocol.TypeGetSnapshot:
		if current.ready.Load() {
			server.enqueue(current, server.snapshotEnvelope())
		}
	}
}

func (server *Server) readWebSocket(current *client, connection *websocket.Conn) {
	defer server.hub.remove(current)
	connection.SetReadLimit(protocol.MaxMessageBytes)
	_ = connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}
		_ = connection.SetReadDeadline(time.Now().Add(45 * time.Second))
		server.processDeviceMessage(current, data)
	}
}

func (server *Server) writeWebSocket(current *client, connection *websocket.Conn) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case data := <-current.send:
			_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := connection.WriteMessage(websocket.TextMessage, data); err != nil {
				server.hub.remove(current)
				return
			}
		case <-ticker.C:
			_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				server.hub.remove(current)
				return
			}
		case <-current.done:
			return
		}
	}
}

func (server *Server) ServeDeviceTransport(ctx context.Context, transport DeviceTransport) error {
	current := &client{
		transport: transport.Name(), closeTransport: transport.Close,
		send: make(chan []byte, server.sendQueue), done: make(chan struct{}),
	}
	defer server.hub.remove(current)
	go func() {
		select {
		case <-ctx.Done():
			current.close()
		case <-current.done:
		}
	}()
	handshakeTimer := time.AfterFunc(15*time.Second, func() {
		if !current.ready.Load() {
			current.close()
		}
	})
	defer handshakeTimer.Stop()
	server.enqueue(current, server.helloEnvelope())
	go server.writeDeviceTransport(ctx, current, transport)
	for {
		data, err := transport.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		server.processDeviceMessage(current, data)
	}
}

func (server *Server) writeDeviceTransport(ctx context.Context, current *client,
	transport DeviceTransport) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	write := func(envelope protocol.Envelope) bool {
		data, err := json.Marshal(envelope)
		if err == nil {
			err = transport.WriteMessage(ctx, data)
		}
		if err != nil {
			server.hub.remove(current)
			return false
		}
		return true
	}
	for {
		select {
		case data := <-current.send:
			if err := transport.WriteMessage(ctx, data); err != nil {
				server.hub.remove(current)
				return
			}
		case <-ticker.C:
			if current.ready.Load() {
				if !write(server.heartbeatEnvelope()) {
					return
				}
			} else if !write(server.helloEnvelope()) {
				return
			}
		case <-current.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
