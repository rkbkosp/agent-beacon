package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
)

func TestMapSnapshotMatchesHerdrPriorityPanel(t *testing.T) {
	snapshot := sessionSnapshot{
		Workspaces: []workspaceInfo{
			{WorkspaceID: "w1", Label: "workspace-alpha", TabCount: 2},
			{WorkspaceID: "w2", Label: "workspace-beta", TabCount: 1},
		},
		Tabs: []tabInfo{
			{TabID: "w1:t1", WorkspaceID: "w1", Label: "1"},
			{TabID: "w1:t2", WorkspaceID: "w1", Label: "review"},
			{TabID: "w2:t1", WorkspaceID: "w2", Label: "1"},
		},
		Agents: []agentInfo{
			{PaneID: "idle", WorkspaceID: "w2", TabID: "w2:t1", Agent: "codex", AgentStatus: "idle", Revision: 8},
			{PaneID: "working", WorkspaceID: "w1", TabID: "w1:t1", Agent: "codex", AgentStatus: "working", Revision: 20},
			{PaneID: "unknown", WorkspaceID: "w2", TabID: "w2:t1", Agent: "codex", AgentStatus: "unknown", Revision: 99},
			{PaneID: "done", WorkspaceID: "w1", TabID: "w1:t2", Agent: "claude", AgentStatus: "done", Revision: 30},
			{PaneID: "blocked", WorkspaceID: "w1", TabID: "w1:t1", Agent: "codex", AgentStatus: "blocked", CustomStatus: "waiting approval", Revision: 10,
				AgentSession: &agentSessionInfo{Source: "herdr:codex", Kind: "id", Value: "session-1"}},
		},
	}

	state, err := mapSnapshot(snapshot, time.Date(2026, 7, 14, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Connected || state.Provider != "herdr" || len(state.Items) != 5 {
		t.Fatalf("state = %+v", state)
	}
	if !state.CodexActive {
		t.Fatal("working Codex session was not marked active")
	}
	wantOrder := []protocol.AgentStatus{
		protocol.AgentBlocked, protocol.AgentDone, protocol.AgentWorking,
		protocol.AgentIdle, protocol.AgentUnknown,
	}
	for index, want := range wantOrder {
		if state.Items[index].Status != want {
			t.Fatalf("item %d status = %q, want %q", index, state.Items[index].Status, want)
		}
	}
	if state.Items[0].DisplayName != "workspace-alpha \u00b7 1" || state.Items[1].DisplayName != "workspace-alpha \u00b7 review" {
		t.Fatalf("workspace/tab labels = %q, %q", state.Items[0].DisplayName, state.Items[1].DisplayName)
	}
	if state.Items[0].CustomStatus != "codex \u00b7 waiting approval" {
		t.Fatalf("blocked secondary = %q", state.Items[0].CustomStatus)
	}
	if state.Items[0].AgentSession == nil || state.Items[0].AgentSession.Value != "session-1" {
		t.Fatalf("agent session was not preserved: %+v", state.Items[0].AgentSession)
	}
}

func TestActiveCodexSessionUsesHerdrIdentityAndWorkingStatus(t *testing.T) {
	tests := []struct {
		name  string
		agent agentInfo
		want  bool
	}{
		{
			name: "session source",
			agent: agentInfo{AgentStatus: "working",
				AgentSession: &agentSessionInfo{Source: "herdr:codex"}},
			want: true,
		},
		{
			name: "session agent",
			agent: agentInfo{AgentStatus: "working",
				AgentSession: &agentSessionInfo{Source: "herdr:other", Agent: "Codex"}},
			want: true,
		},
		{name: "top-level fallback", agent: agentInfo{Agent: "CODEX", AgentStatus: "working"}, want: true},
		{name: "blocked Codex", agent: agentInfo{Agent: "codex", AgentStatus: "blocked"}},
		{name: "idle Codex", agent: agentInfo{Agent: "codex", AgentStatus: "idle"}},
		{name: "working other agent", agent: agentInfo{Agent: "claude", AgentStatus: "working"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := activeCodexSession(test.agent); got != test.want {
				t.Fatalf("activeCodexSession(%+v) = %v, want %v", test.agent, got, test.want)
			}
		})
	}
}

func TestMapSnapshotAggregatesAnyWorkingCodexSession(t *testing.T) {
	tests := []struct {
		name   string
		agents []agentInfo
		want   bool
	}{
		{name: "empty"},
		{name: "blocked Codex", agents: []agentInfo{{PaneID: "p1", Agent: "codex", AgentStatus: "blocked"}}},
		{name: "working non-Codex", agents: []agentInfo{{PaneID: "p1", Agent: "claude", AgentStatus: "working"}}},
		{
			name: "one working Codex among inactive sessions",
			agents: []agentInfo{
				{PaneID: "p1", Agent: "codex", AgentStatus: "idle"},
				{PaneID: "p2", AgentStatus: "working", AgentSession: &agentSessionInfo{Source: "herdr:codex"}},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := mapSnapshot(sessionSnapshot{Agents: test.agents}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if state.CodexActive != test.want {
				t.Fatalf("CodexActive = %v, want %v", state.CodexActive, test.want)
			}
		})
	}
}

func TestTransitionNotificationsUseRealHerdrStateChanges(t *testing.T) {
	now := time.Now()
	previous := protocol.AgentsState{Provider: "herdr", Connected: true, Items: []protocol.AgentItem{
		{PaneID: "p1", DisplayName: "Build", Status: protocol.AgentWorking, Revision: 1},
		{PaneID: "p2", DisplayName: "Review", Status: protocol.AgentBlocked, Revision: 2},
	}}
	current := protocol.AgentsState{Provider: "herdr", Connected: true, Items: []protocol.AgentItem{
		{PaneID: "p1", DisplayName: "Build", Status: protocol.AgentBlocked, CustomStatus: "等待批准", Revision: 3},
		{PaneID: "p2", DisplayName: "Review", Status: protocol.AgentDone, Revision: 4},
	}}
	notifications := transitionNotifications(previous, current, now)
	if len(notifications) != 2 || notifications[0].Kind != "agent.blocked" || notifications[1].Kind != "agent.done" {
		t.Fatalf("notifications = %+v", notifications)
	}
	for _, notification := range notifications {
		if notification.Source != "herdr" || notification.DedupeKey == "" || notification.ExpiresAt.Before(now) {
			t.Fatalf("invalid notification = %+v", notification)
		}
	}
}

func TestTransitionNotificationsDistinguishRepeatedEpisodesWithoutHerdrRevision(t *testing.T) {
	firstAt := time.Date(2026, time.July, 15, 11, 15, 50, 0, time.Local)
	working := protocol.AgentsState{Provider: "herdr", Connected: true, Items: []protocol.AgentItem{
		{PaneID: "p1", DisplayName: "Review", Status: protocol.AgentWorking, Revision: 0,
			AgentSession: &protocol.AgentSession{Source: "herdr:codex", Kind: "id", Value: "session-1"}},
	}}
	blocked := protocol.AgentsState{Provider: "herdr", Connected: true, Items: []protocol.AgentItem{
		{PaneID: "p1", DisplayName: "Review", Status: protocol.AgentBlocked, Revision: 0,
			AgentSession: &protocol.AgentSession{Source: "herdr:codex", Kind: "id", Value: "session-1"}},
	}}

	first := transitionNotifications(working, blocked, firstAt)
	second := transitionNotifications(working, blocked, firstAt.Add(time.Second))
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("notification counts = %d, %d", len(first), len(second))
	}
	if first[0].DedupeKey == second[0].DedupeKey {
		t.Fatalf("repeated blocked episodes shared dedupe key %q", first[0].DedupeKey)
	}
}

func TestProviderSnapshotsSubscribesAndResyncsOnEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("/tmp", "agent-beacon-herdr-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	socketPath := filepath.Join(tempDir, "herdr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	requests := make(chan map[string]any, 8)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		snapshotCount := 0
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			var request map[string]any
			decodeErr := json.NewDecoder(bufio.NewReader(connection)).Decode(&request)
			if decodeErr != nil {
				connection.Close()
				continue
			}
			requests <- request
			switch request["method"] {
			case "session.snapshot":
				status := "working"
				if snapshotCount > 0 {
					status = "blocked"
				}
				snapshotCount++
				response := map[string]any{
					"id": request["id"],
					"result": map[string]any{"type": "session_snapshot", "snapshot": map[string]any{
						"version": "0.7.3", "protocol": 16,
						"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "project", "tab_count": 1}},
						"tabs":       []any{map[string]any{"tab_id": "w1:t1", "workspace_id": "w1", "label": "1"}},
						"agents":     []any{map[string]any{"terminal_id": "term-1", "workspace_id": "w1", "tab_id": "w1:t1", "pane_id": "w1:p1", "agent": "codex", "agent_status": status, "focused": false, "revision": snapshotCount}},
					}},
				}
				_ = json.NewEncoder(connection).Encode(response)
				_ = connection.Close()
			case "events.subscribe":
				encoder := json.NewEncoder(connection)
				_ = encoder.Encode(map[string]any{"id": request["id"], "result": map[string]any{"type": "subscription_started"}})
				_ = encoder.Encode(map[string]any{"event": "pane.agent_status_changed", "data": map[string]any{"pane_id": "w1:p1", "agent_status": "blocked"}})
				_ = connection.Close()
			default:
				_ = connection.Close()
			}
		}
	}()

	provider := New(Config{
		SocketPath: socketPath, ReconnectMax: 20 * time.Millisecond,
		FullResyncInterval: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan providers.Update, 4)
	errCh := make(chan error, 1)
	go func() { errCh <- provider.Start(ctx, updates) }()

	first := receiveAgentsUpdate(t, updates)
	if first.Items[0].Status != protocol.AgentWorking {
		t.Fatalf("first status = %q", first.Items[0].Status)
	}
	second := receiveAgentsUpdate(t, updates)
	if second.Items[0].Status != protocol.AgentBlocked {
		t.Fatalf("second status = %q", second.Items[0].Status)
	}

	var subscription map[string]any
	deadline := time.After(time.Second)
	for subscription == nil {
		select {
		case request := <-requests:
			if request["method"] == "events.subscribe" {
				subscription = request
			}
		case <-deadline:
			t.Fatal("events.subscribe request was not observed")
		}
	}
	encoded, _ := json.Marshal(subscription)
	for _, required := range []string{"pane.agent_status_changed", "pane.agent_detected", "pane.created", "pane.closed", "pane.exited"} {
		if !containsJSONText(encoded, required) {
			t.Fatalf("subscription missing %q: %s", required, encoded)
		}
	}

	cancel()
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("Start returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider did not stop after context cancellation")
	}
	_ = listener.Close()
	<-serverDone
}

func receiveAgentsUpdate(t *testing.T, updates <-chan providers.Update) protocol.AgentsState {
	t.Helper()
	select {
	case update := <-updates:
		if update.Patch.Agents == nil {
			t.Fatal("Herdr update did not contain agents")
		}
		return *update.Patch.Agents
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Herdr update")
		return protocol.AgentsState{}
	}
}

func containsJSONText(data []byte, value string) bool {
	for index := 0; index+len(value) <= len(data); index++ {
		if string(data[index:index+len(value)]) == value {
			return true
		}
	}
	return false
}
