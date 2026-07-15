package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
)

const maxHerdrLineBytes = 2 * 1024 * 1024

type Config struct {
	SocketPath         string
	Session            string
	ReconnectMax       time.Duration
	FullResyncInterval time.Duration
	RequestTimeout     time.Duration
}

type Provider struct {
	config Config

	mu       sync.RWMutex
	health   providers.Health
	last     protocol.AgentsState
	hasState bool
}

type sessionSnapshot struct {
	Version    string          `json:"version"`
	Protocol   uint32          `json:"protocol"`
	Workspaces []workspaceInfo `json:"workspaces"`
	Tabs       []tabInfo       `json:"tabs"`
	Agents     []agentInfo     `json:"agents"`
}

type workspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	TabCount    int    `json:"tab_count"`
}

type tabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type agentInfo struct {
	TerminalID   string            `json:"terminal_id"`
	Name         string            `json:"name,omitempty"`
	Agent        string            `json:"agent,omitempty"`
	Title        string            `json:"title,omitempty"`
	DisplayAgent string            `json:"display_agent,omitempty"`
	AgentStatus  string            `json:"agent_status"`
	CustomStatus string            `json:"custom_status,omitempty"`
	AgentSession *agentSessionInfo `json:"agent_session,omitempty"`
	WorkspaceID  string            `json:"workspace_id"`
	TabID        string            `json:"tab_id"`
	PaneID       string            `json:"pane_id"`
	Focused      bool              `json:"focused"`
	Revision     uint64            `json:"revision"`
}

type agentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent,omitempty"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

type snapshotResponse struct {
	ID     string `json:"id"`
	Result struct {
		Type     string          `json:"type"`
		Snapshot sessionSnapshot `json:"snapshot"`
	} `json:"result"`
	Error json.RawMessage `json:"error,omitempty"`
}

type subscriptionResponse struct {
	ID     string `json:"id"`
	Result struct {
		Type string `json:"type"`
	} `json:"result"`
	Error json.RawMessage `json:"error,omitempty"`
}

func New(config Config) *Provider {
	if config.ReconnectMax <= 0 {
		config.ReconnectMax = 30 * time.Second
	}
	if config.FullResyncInterval <= 0 {
		config.FullResyncInterval = 60 * time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 5 * time.Second
	}
	config.SocketPath = resolveSocketPath(config)
	return &Provider{
		config: config,
		health: providers.Health{Healthy: false, Detail: "waiting for Herdr socket"},
	}
}

func (*Provider) Name() string { return "herdr" }

func (provider *Provider) Start(ctx context.Context, out chan<- providers.Update) error {
	backoff := time.Second
	for {
		state, paneIDs, err := provider.fetchSnapshot(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			provider.setHealth(false, err.Error())
			if !provider.publishDisconnected(ctx, out) || !waitContext(ctx, backoff) {
				return ctx.Err()
			}
			backoff *= 2
			if backoff > provider.config.ReconnectMax {
				backoff = provider.config.ReconnectMax
			}
			continue
		}

		backoff = time.Second
		provider.setHealth(true, fmt.Sprintf("connected to %s", provider.config.SocketPath))
		if !provider.publishIfChanged(ctx, out, state) {
			return ctx.Err()
		}

		if err := provider.subscribeUntilResync(ctx, paneIDs); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			provider.setHealth(false, err.Error())
			if !provider.publishDisconnected(ctx, out) || !waitContext(ctx, backoff) {
				return ctx.Err()
			}
		}
	}
}

func (provider *Provider) Snapshot(ctx context.Context) (protocol.StatePatch, error) {
	state, _, err := provider.fetchSnapshot(ctx)
	if err != nil {
		provider.setHealth(false, err.Error())
		return protocol.StatePatch{}, err
	}
	provider.setHealth(true, fmt.Sprintf("connected to %s", provider.config.SocketPath))
	return protocol.StatePatch{Agents: &state}, nil
}

func (provider *Provider) Health(context.Context) providers.Health {
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	return provider.health
}

func (provider *Provider) fetchSnapshot(ctx context.Context) (protocol.AgentsState, []string, error) {
	connection, err := provider.dial(ctx)
	if err != nil {
		return protocol.AgentsState{}, nil, fmt.Errorf("connect Herdr socket: %w", err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	_ = connection.SetDeadline(time.Now().Add(provider.config.RequestTimeout))

	request := map[string]any{
		"id": "agent-beacon-snapshot", "method": "session.snapshot", "params": map[string]any{},
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return protocol.AgentsState{}, nil, fmt.Errorf("request Herdr snapshot: %w", err)
	}
	var response snapshotResponse
	if err := decodeLine(connection, &response); err != nil {
		return protocol.AgentsState{}, nil, fmt.Errorf("read Herdr snapshot: %w", err)
	}
	if len(response.Error) > 0 || response.Result.Type != "session_snapshot" {
		return protocol.AgentsState{}, nil, fmt.Errorf("unexpected Herdr snapshot response")
	}
	state, err := mapSnapshot(response.Result.Snapshot, time.Now())
	if err != nil {
		return protocol.AgentsState{}, nil, err
	}
	paneIDs := make([]string, 0, len(response.Result.Snapshot.Agents))
	for _, agent := range response.Result.Snapshot.Agents {
		if agent.PaneID != "" {
			paneIDs = append(paneIDs, agent.PaneID)
		}
	}
	return state, paneIDs, nil
}

func (provider *Provider) subscribeUntilResync(ctx context.Context, paneIDs []string) error {
	connection, err := provider.dial(ctx)
	if err != nil {
		return fmt.Errorf("connect Herdr event stream: %w", err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()

	subscriptions := []map[string]any{
		{"type": "pane.agent_detected"},
		{"type": "pane.created"},
		{"type": "pane.closed"},
		{"type": "pane.exited"},
		{"type": "pane.focused"},
		{"type": "workspace.created"},
		{"type": "workspace.updated"},
		{"type": "workspace.renamed"},
		{"type": "workspace.closed"},
		{"type": "tab.created"},
		{"type": "tab.closed"},
		{"type": "tab.renamed"},
	}
	for _, paneID := range paneIDs {
		subscriptions = append(subscriptions, map[string]any{
			"type": "pane.agent_status_changed", "pane_id": paneID,
		})
	}
	request := map[string]any{
		"id": "agent-beacon-events", "method": "events.subscribe",
		"params": map[string]any{"subscriptions": subscriptions},
	}
	_ = connection.SetDeadline(time.Now().Add(provider.config.RequestTimeout))
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return fmt.Errorf("subscribe to Herdr events: %w", err)
	}
	reader := bufio.NewReader(connection)
	var response subscriptionResponse
	if err := decodeReaderLine(reader, &response); err != nil {
		return fmt.Errorf("read Herdr subscription response: %w", err)
	}
	if len(response.Error) > 0 || response.Result.Type != "subscription_started" {
		return fmt.Errorf("unexpected Herdr subscription response")
	}

	_ = connection.SetReadDeadline(time.Now().Add(provider.config.FullResyncInterval))
	var event json.RawMessage
	if err := decodeReaderLine(reader, &event); err != nil {
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			return nil
		}
		return fmt.Errorf("read Herdr event stream: %w", err)
	}
	return nil
}

func (provider *Provider) dial(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", provider.config.SocketPath)
}

func (provider *Provider) publishIfChanged(ctx context.Context, out chan<- providers.Update,
	state protocol.AgentsState) bool {
	provider.mu.Lock()
	previous := provider.last
	hadState := provider.hasState
	changed := !provider.hasState || provider.last.Connected != state.Connected ||
		!reflect.DeepEqual(provider.last.Items, state.Items)
	if changed {
		provider.last = state
		provider.hasState = true
	}
	provider.mu.Unlock()
	if !changed {
		return true
	}
	updates := []providers.Update{{Patch: protocol.StatePatch{Agents: &state}}}
	if hadState && previous.Connected && state.Connected {
		for _, notification := range transitionNotifications(previous, state, time.Now()) {
			updates = append(updates, providers.Update{Notification: notification})
		}
	}
	for _, update := range updates {
		select {
		case out <- update:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func transitionNotifications(previous, current protocol.AgentsState, now time.Time) []*protocol.Notification {
	before := make(map[string]protocol.AgentItem, len(previous.Items))
	for _, item := range previous.Items {
		before[item.PaneID] = item
	}
	var result []*protocol.Notification
	for _, item := range current.Items {
		old, exists := before[item.PaneID]
		if !exists || old.Status == item.Status {
			continue
		}
		session := "pane"
		if item.AgentSession != nil && item.AgentSession.Value != "" {
			session = item.AgentSession.Value
		}
		switch {
		case item.Status == protocol.AgentBlocked && old.Status != protocol.AgentBlocked:
			detail := item.DisplayName + " · " + firstNonEmpty(item.CustomStatus, "BLOCKED")
			result = append(result, agentNotification(now, old, item, "agent.blocked", protocol.ThemeYellow,
				protocol.UrgencyAttention, 75, session, "Agent 需要关注", detail, 7000, 30*time.Minute))
		case item.Status == protocol.AgentDone && (old.Status == protocol.AgentWorking || old.Status == protocol.AgentBlocked):
			detail := item.DisplayName + " · 等待查看"
			result = append(result, agentNotification(now, old, item, "agent.done", protocol.ThemeGreen,
				protocol.UrgencyNormal, 50, session, "Agent 已完成", detail, 4000, time.Minute))
		}
	}
	return result
}

func agentNotification(now time.Time, previous, item protocol.AgentItem, kind string, theme protocol.Theme,
	urgency protocol.Urgency, priority uint8, session, title, detail string, display uint32,
	ttl time.Duration) *protocol.Notification {
	transitionRevision := fmt.Sprintf("%d", item.Revision)
	if item.Revision <= previous.Revision {
		// Herdr 0.7.3 currently reports revision=0 for agent snapshots. A local
		// transition stamp keeps separate blocked/done episodes from collapsing
		// to the same device-side dedupe key.
		transitionRevision = fmt.Sprintf("local-%d", now.UnixNano())
	}
	return &protocol.Notification{
		Category: protocol.CategoryAgent, Kind: kind, Source: "herdr", SubjectID: item.PaneID,
		Theme: theme, Urgency: urgency, Priority: priority,
		DedupeKey:    fmt.Sprintf("agent:%s:%s:%s:%s", item.PaneID, session, strings.TrimPrefix(kind, "agent."), transitionRevision),
		SupersedeKey: "agent:" + item.PaneID, Title: title, Detail: detail, SourceLabel: "Herdr",
		DisplayMS: display, ExpiresAt: now.Add(ttl), ReplayAfterInterrupt: urgency != protocol.UrgencyNormal,
		MaxReplays: 1,
	}
}

func (provider *Provider) publishDisconnected(ctx context.Context, out chan<- providers.Update) bool {
	provider.mu.RLock()
	state := provider.last
	hasState := provider.hasState
	provider.mu.RUnlock()
	if !hasState {
		state = protocol.AgentsState{Provider: "herdr"}
	}
	state.Connected = false
	state.UpdatedAt = time.Now()
	return provider.publishIfChanged(ctx, out, state)
}

func (provider *Provider) setHealth(healthy bool, detail string) {
	provider.mu.Lock()
	provider.health = providers.Health{Healthy: healthy, Detail: detail}
	provider.mu.Unlock()
}

func mapSnapshot(snapshot sessionSnapshot, now time.Time) (protocol.AgentsState, error) {
	workspaces := make(map[string]workspaceInfo, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		workspaces[workspace.WorkspaceID] = workspace
	}
	tabs := make(map[string]tabInfo, len(snapshot.Tabs))
	tabCounts := make(map[string]int, len(snapshot.Workspaces))
	for _, tab := range snapshot.Tabs {
		tabs[tab.TabID] = tab
		tabCounts[tab.WorkspaceID]++
	}

	items := make([]protocol.AgentItem, 0, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		status := protocol.AgentStatus(agent.AgentStatus)
		if agent.PaneID == "" || !validAgentStatus(status) {
			return protocol.AgentsState{}, fmt.Errorf("invalid Herdr agent pane=%q status=%q", agent.PaneID, agent.AgentStatus)
		}
		workspace := workspaces[agent.WorkspaceID]
		displayName := workspace.Label
		if displayName == "" {
			displayName = firstNonEmpty(agent.DisplayAgent, agent.Name, agent.Agent, agent.Title, agent.PaneID)
		}
		if workspace.TabCount > 1 || tabCounts[agent.WorkspaceID] > 1 {
			if tab := tabs[agent.TabID]; tab.Label != "" {
				displayName += " \u00b7 " + tab.Label
			}
		}
		secondary := agent.Agent
		if agent.CustomStatus != "" {
			secondary = strings.TrimSpace(strings.Join(nonEmpty(agent.Agent, agent.CustomStatus), " \u00b7 "))
		}
		item := protocol.AgentItem{
			PaneID: agent.PaneID, TerminalID: agent.TerminalID,
			WorkspaceID: agent.WorkspaceID, TabID: agent.TabID, Agent: agent.Agent,
			DisplayName: displayName, Status: status, CustomStatus: secondary,
			Title: agent.Title, Focused: agent.Focused, Revision: agent.Revision,
		}
		if agent.AgentSession != nil {
			item.AgentSession = &protocol.AgentSession{
				Source: agent.AgentSession.Source, Kind: agent.AgentSession.Kind,
				Value: agent.AgentSession.Value,
			}
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(left, right int) bool {
		leftItem, rightItem := items[left], items[right]
		if statusPriority(leftItem.Status) != statusPriority(rightItem.Status) {
			return statusPriority(leftItem.Status) > statusPriority(rightItem.Status)
		}
		if leftItem.Revision != rightItem.Revision {
			return leftItem.Revision > rightItem.Revision
		}
		if leftItem.Focused != rightItem.Focused {
			return leftItem.Focused
		}
		return leftItem.DisplayName < rightItem.DisplayName
	})

	return protocol.AgentsState{
		Provider: "herdr", Connected: true, UpdatedAt: now, Items: items,
	}, nil
}

func validAgentStatus(status protocol.AgentStatus) bool {
	switch status {
	case protocol.AgentBlocked, protocol.AgentDone, protocol.AgentWorking,
		protocol.AgentIdle, protocol.AgentUnknown:
		return true
	default:
		return false
	}
}

func statusPriority(status protocol.AgentStatus) int {
	switch status {
	case protocol.AgentBlocked:
		return 5
	case protocol.AgentDone:
		return 4
	case protocol.AgentWorking:
		return 3
	case protocol.AgentIdle:
		return 2
	default:
		return 1
	}
}

func resolveSocketPath(config Config) string {
	if config.SocketPath != "" {
		return config.SocketPath
	}
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".config", "herdr")
	if config.Session != "" && config.Session != "default" {
		return filepath.Join(base, "sessions", config.Session, "herdr.sock")
	}
	return filepath.Join(base, "herdr.sock")
}

func decodeLine(connection net.Conn, target any) error {
	return decodeReaderLine(bufio.NewReader(connection), target)
}

func decodeReaderLine(reader *bufio.Reader, target any) error {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	if len(line) > maxHerdrLineBytes {
		return fmt.Errorf("Herdr message exceeds %d bytes", maxHerdrLineBytes)
	}
	return json.Unmarshal(line, target)
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "-"
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
