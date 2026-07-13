package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
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
	id         string
	connection *websocket.Conn
	send       chan []byte
	done       chan struct{}
	closeOnce  sync.Once
	ready      atomic.Bool
}

func (client *client) close() {
	client.closeOnce.Do(func() {
		close(client.done)
		_ = client.connection.Close()
	})
}

type hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
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
	ids := make([]string, 0, len(hub.clients))
	for current := range hub.clients {
		ids = append(ids, current.id)
	}
	return ids
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
	envelope, _ := protocol.NewEnvelope(server.nextID("snapshot"), protocol.TypeSnapshot, server.store.Revision(), time.Now().UTC(), snapshot)
	return envelope
}

func (server *Server) helloEnvelope() protocol.Envelope {
	envelope, _ := protocol.NewEnvelope(server.nextID("hello"), protocol.TypeHello, server.store.Revision(), time.Now().UTC(), protocol.Hello{
		Role: "server", ProtocolVersion: protocol.Version, BridgeVersion: "bridge-v2",
	})
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
	writeJSON(writer, http.StatusOK, map[string]any{"devices": server.hub.deviceIDs()})
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
	server.applyPatch(fixture.Patch)
	patchRevision := server.store.NextRevision()
	patchEnvelope, err := protocol.NewEnvelope(server.nextID("patch"), protocol.TypeStatePatch, patchRevision, time.Now().UTC(), fixture.Patch)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "invalid fixture patch"})
		return
	}
	server.broadcast(patchEnvelope)
	response := map[string]any{"status": "accepted", "fixture": request.PathValue("name"), "revision": patchRevision}
	if fixture.Notification != nil {
		event, envelopeError := protocol.NewEnvelope(server.nextID("evt"), protocol.TypeNotification, 0, time.Now().UTC(), *fixture.Notification)
		if envelopeError != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "invalid fixture notification"})
			return
		}
		result, accepted := server.store.AcceptEvent(event, time.Now())
		response["event_status"] = result
		if result == state.EventAccepted {
			server.broadcast(accepted)
			response["event_id"] = accepted.ID
		}
	}
	writeJSON(writer, http.StatusAccepted, response)
}

func (server *Server) PublishProviderUpdate(update providers.Update) error {
	if update.Patch.Clock != nil || update.Patch.Codex != nil || update.Patch.Agents != nil ||
		update.Patch.Weather != nil || update.Patch.System != nil {
		system := server.applyPatch(update.Patch)
		if update.Patch.System == nil {
			update.Patch.System = &system
		}
		revision := server.store.NextRevision()
		envelope, err := protocol.NewEnvelope(server.nextID("patch"), protocol.TypeStatePatch,
			revision, time.Now().UTC(), update.Patch)
		if err != nil {
			return fmt.Errorf("encode provider patch: %w", err)
		}
		server.broadcast(envelope)
	}
	if update.Notification != nil {
		envelope, err := protocol.NewEnvelope(server.nextID("evt"), protocol.TypeNotification,
			0, time.Now().UTC(), *update.Notification)
		if err != nil {
			return fmt.Errorf("encode provider notification: %w", err)
		}
		result, accepted := server.store.AcceptEvent(envelope, time.Now())
		if result == state.EventAccepted {
			server.broadcast(accepted)
		}
	}
	return nil
}

func (server *Server) applyPatch(patch protocol.StatePatch) protocol.SystemState {
	server.snapshotMu.Lock()
	defer server.snapshotMu.Unlock()
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
	return server.snapshot.System
}

func overallFreshness(snapshot protocol.Snapshot) protocol.Freshness {
	values := make([]protocol.Freshness, 0, len(snapshot.Codex.Homes)+4)
	for _, home := range snapshot.Codex.Homes {
		values = append(values, home.Freshness)
	}
	values = append(values, snapshot.Codex.Relay.Freshness, snapshot.Weather.Current.Freshness)
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
	current := &client{id: deviceID, connection: connection, send: make(chan []byte, server.sendQueue), done: make(chan struct{})}
	server.hub.add(current)
	server.enqueue(current, server.helloEnvelope())
	go server.writePump(current)
	go server.readPump(current)
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

func (server *Server) readPump(current *client) {
	defer server.hub.remove(current)
	current.connection.SetReadLimit(protocol.MaxMessageBytes)
	_ = current.connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	current.connection.SetPongHandler(func(string) error {
		return current.connection.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	for {
		_, data, err := current.connection.ReadMessage()
		if err != nil {
			return
		}
		_ = current.connection.SetReadDeadline(time.Now().Add(45 * time.Second))
		message, err := protocol.DecodeDeviceMessage(data)
		if err != nil {
			continue
		}
		if message.ACK != nil {
			if message.ACK.DeviceID == current.id {
				server.store.RecordACK(*message.ACK, time.Now().UTC())
			}
			continue
		}
		switch message.Envelope.Type {
		case protocol.TypeHello:
			hello, decodeError := protocol.DecodePayload[protocol.Hello](*message.Envelope)
			if decodeError == nil && hello.Role == "device" && hello.DeviceID == current.id {
				server.enqueue(current, server.snapshotEnvelope())
				current.ready.Store(true)
			}
		case protocol.TypeGetSnapshot:
			server.enqueue(current, server.snapshotEnvelope())
		}
	}
}

func (server *Server) writePump(current *client) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case data := <-current.send:
			_ = current.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := current.connection.WriteMessage(websocket.TextMessage, data); err != nil {
				server.hub.remove(current)
				return
			}
		case <-ticker.C:
			_ = current.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := current.connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				server.hub.remove(current)
				return
			}
		case <-current.done:
			return
		}
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
