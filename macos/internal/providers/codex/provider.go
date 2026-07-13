package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
	"agent-beacon/internal/providers/relaybalance"
)

type RelayFetcher interface {
	Fetch(context.Context) (relaybalance.Result, error)
}

type Provider struct {
	config      config.CodexProviderConfig
	relayConfig config.RelayBalanceConfig
	relay       RelayFetcher
	now         func() time.Time

	mu                   sync.Mutex
	homes                map[string]*homeRuntime
	relayState           protocol.RelayState
	relayOK              bool
	relayStartedAt       time.Time
	relayInvalidNotified bool
	relayStaleNotified   bool
	initialized          bool
	health               providers.Health
}

type homeRuntime struct {
	state          protocol.CodexHome
	hasSuccess     bool
	startedAt      time.Time
	lastSuccess    time.Time
	staleNotified  bool
	thresholdArmed map[int]bool
}

type homeResult struct {
	home   config.CodexHomeConfig
	output AdapterOutput
	err    error
}

func New(providerConfig config.CodexProviderConfig, relayConfig config.RelayBalanceConfig, relay RelayFetcher) *Provider {
	now := time.Now()
	value := &Provider{
		config: providerConfig, relayConfig: relayConfig, relay: relay, now: time.Now,
		homes:          make(map[string]*homeRuntime, len(providerConfig.Homes)),
		relayState:     protocol.RelayState{Unit: "USD", UpdatedAt: now, Freshness: protocol.FreshnessUnknown},
		relayStartedAt: now,
		health:         providers.Health{Healthy: false, Detail: "waiting for Codex and relay data"},
	}
	for _, home := range providerConfig.Homes {
		value.homes[home.ID] = &homeRuntime{startedAt: now, thresholdArmed: make(map[int]bool), state: protocol.CodexHome{
			ID: home.ID, Label: home.Label, UpdatedAt: now, Freshness: protocol.FreshnessUnknown,
		}}
	}
	return value
}

func (*Provider) Name() string { return "codex+relay" }

func (provider *Provider) Snapshot(ctx context.Context) (protocol.StatePatch, error) {
	updates, err := provider.refreshAll(ctx, false)
	provider.mu.Lock()
	provider.initialized = true
	state := provider.stateLocked()
	provider.mu.Unlock()
	_ = updates
	return protocol.StatePatch{Codex: &state}, err
}

func (provider *Provider) Start(ctx context.Context, out chan<- providers.Update) error {
	provider.mu.Lock()
	initialized := provider.initialized
	state := provider.stateLocked()
	provider.mu.Unlock()
	if !initialized {
		updates, _ := provider.refreshAll(ctx, false)
		if err := sendUpdates(ctx, out, updates); err != nil {
			return err
		}
	} else if err := sendUpdates(ctx, out, []providers.Update{{Patch: protocol.StatePatch{Codex: &state}}}); err != nil {
		return err
	}
	homeTicker := time.NewTicker(provider.config.RefreshInterval)
	defer homeTicker.Stop()
	var relayTicker *time.Ticker
	var relayChannel <-chan time.Time
	if provider.relayConfig.Enabled {
		relayTicker = time.NewTicker(provider.relayConfig.RefreshInterval)
		relayChannel = relayTicker.C
		defer relayTicker.Stop()
	}
	for {
		var updates []providers.Update
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-homeTicker.C:
			updates, _ = provider.refreshHomes(ctx, true)
		case <-relayChannel:
			updates, _ = provider.refreshRelay(ctx, true)
		}
		if err := sendUpdates(ctx, out, updates); err != nil {
			return err
		}
	}
}

func (provider *Provider) Health(context.Context) providers.Health {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.health
}

func (provider *Provider) refreshAll(ctx context.Context, notify bool) ([]providers.Update, error) {
	homeUpdates, homeErr := provider.refreshHomes(ctx, notify)
	relayUpdates, relayErr := provider.refreshRelay(ctx, notify)
	updates := append(homeUpdates, relayUpdates...)
	return updates, errors.Join(homeErr, relayErr)
}

func (provider *Provider) refreshHomes(ctx context.Context, notify bool) ([]providers.Update, error) {
	results := make(chan homeResult, len(provider.config.Homes))
	for _, home := range provider.config.Homes {
		home := home
		go func() {
			output, err := runAdapter(ctx, provider.config.Adapter, home)
			results <- homeResult{home: home, output: output, err: err}
		}()
	}
	var combined error
	var notifications []*protocol.Notification
	for range provider.config.Homes {
		result := <-results
		provider.mu.Lock()
		runtime := provider.homes[result.home.ID]
		before := runtime.state
		wasSuccessful := runtime.hasSuccess
		if result.err != nil {
			combined = errors.Join(combined, fmt.Errorf("%s: %w", result.home.ID, result.err))
			runtime.state.UpdatedAt = provider.now()
			if runtime.hasSuccess && provider.now().Sub(runtime.lastSuccess) < provider.config.StaleAfter {
				runtime.state.Freshness = protocol.FreshnessCached
			} else if runtime.hasSuccess {
				runtime.state.Freshness = protocol.FreshnessStale
			} else if provider.now().Sub(runtime.startedAt) >= provider.config.StaleAfter {
				runtime.state.Freshness = protocol.FreshnessStale
			} else {
				runtime.state.Freshness = protocol.FreshnessUnknown
			}
			if notify && runtime.state.Freshness == protocol.FreshnessStale && !runtime.staleNotified {
				runtime.staleNotified = true
				notifications = append(notifications, quotaStaleNotification(runtime.state, provider.now()))
			}
		} else {
			runtime.state = protocol.CodexHome{
				ID: result.home.ID, Label: result.home.Label,
				WeeklyRemainingPercent:    result.output.Weekly.RemainingPercent,
				WeeklyResetAt:             result.output.Weekly.ResetAt,
				ResetCardsAvailable:       result.output.ResetCards.Available,
				NearestResetCardExpiresAt: result.output.ResetCards.NearestExpiresAt,
				UpdatedAt:                 result.output.ObservedAt, Freshness: protocol.FreshnessFresh,
			}
			runtime.hasSuccess = true
			runtime.lastSuccess = provider.now()
			runtime.staleNotified = false
			if notify && wasSuccessful {
				notifications = append(notifications, quotaTransitionNotifications(runtime, before, runtime.state, provider.now())...)
			} else if !wasSuccessful {
				for _, threshold := range []int{30, 15, 5} {
					runtime.thresholdArmed[threshold] = runtime.state.WeeklyRemainingPercent > threshold
				}
			}
		}
		provider.mu.Unlock()
	}
	provider.mu.Lock()
	provider.updateHealthLocked(combined)
	state := provider.stateLocked()
	provider.mu.Unlock()
	updates := []providers.Update{{Patch: protocol.StatePatch{Codex: &state}}}
	for _, notification := range notifications {
		updates = append(updates, providers.Update{Notification: notification})
	}
	return updates, combined
}

func (provider *Provider) refreshRelay(ctx context.Context, notify bool) ([]providers.Update, error) {
	if !provider.relayConfig.Enabled {
		return nil, nil
	}
	if provider.relay == nil {
		return nil, errors.New("relay provider is enabled without a client")
	}
	result, err := provider.relay.Fetch(ctx)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	now := provider.now()
	var notification *protocol.Notification
	if errors.Is(err, relaybalance.ErrInvalidCredentials) {
		provider.relayState = protocol.RelayState{Unit: "USD", IsValid: false, UpdatedAt: now, Freshness: protocol.FreshnessFresh}
		provider.relayOK = false
		if notify && !provider.relayInvalidNotified {
			provider.relayInvalidNotified = true
			notification = relayNotification(now, "system.relay_key_invalid", protocol.ThemeYellow, protocol.UrgencyAttention, 70,
				"system:relay:key-invalid", "中转凭证无效", "0-0 API 凭证")
		}
	} else if err != nil {
		if provider.relayOK && now.Sub(provider.relayState.UpdatedAt) < provider.relayConfig.StaleAfter {
			provider.relayState.Freshness = protocol.FreshnessCached
		} else if provider.relayOK {
			provider.relayState.Freshness = protocol.FreshnessStale
		} else if now.Sub(provider.relayStartedAt) >= provider.relayConfig.StaleAfter {
			provider.relayState.Freshness = protocol.FreshnessStale
		} else {
			provider.relayState.Freshness = protocol.FreshnessUnknown
		}
		if notify && provider.relayState.Freshness == protocol.FreshnessStale && !provider.relayStaleNotified {
			provider.relayStaleNotified = true
			notification = relayNotification(now, "system.relay_stale", protocol.ThemeYellow, protocol.UrgencyNormal, 42,
				"system:relay:stale", "中转余额不可用", "保留上次成功余额")
		}
	} else {
		remaining := result.Remaining
		wasUnhealthy := provider.relayInvalidNotified || provider.relayStaleNotified
		provider.relayState = protocol.RelayState{
			Remaining: &remaining, Unit: result.Unit, IsValid: true,
			UpdatedAt: result.FetchedAt, Freshness: protocol.FreshnessFresh,
		}
		provider.relayOK = true
		provider.relayInvalidNotified = false
		provider.relayStaleNotified = false
		if notify && wasUnhealthy {
			notification = relayNotification(now, "system.relay_restored", protocol.ThemeGreen, protocol.UrgencyNormal, 30,
				"system:relay:restored", "中转余额已恢复", "0-0 API 可用")
		}
	}
	provider.updateHealthLocked(err)
	state := provider.stateLocked()
	update := providers.Update{Patch: protocol.StatePatch{Codex: &state}, Notification: notification}
	return []providers.Update{update}, err
}

func (provider *Provider) stateLocked() protocol.CodexState {
	homes := make([]protocol.CodexHome, 0, len(provider.config.Homes))
	for _, configured := range provider.config.Homes {
		homes = append(homes, provider.homes[configured.ID].state)
	}
	return protocol.CodexState{Homes: homes, Relay: provider.relayState}
}

func (provider *Provider) updateHealthLocked(err error) {
	if err != nil {
		provider.health = providers.Health{Healthy: false, Detail: err.Error()}
		return
	}
	for _, home := range provider.homes {
		if !home.hasSuccess || home.state.Freshness != protocol.FreshnessFresh {
			provider.health = providers.Health{Healthy: false, Detail: "one or more Codex homes are not fresh"}
			return
		}
	}
	if provider.relayConfig.Enabled && (!provider.relayOK || provider.relayState.Freshness != protocol.FreshnessFresh) {
		provider.health = providers.Health{Healthy: false, Detail: "relay balance is not fresh"}
		return
	}
	provider.health = providers.Health{Healthy: true, Detail: "Codex homes and relay balance are available"}
}

func runAdapter(ctx context.Context, adapter config.CodexAdapterConfig, home config.CodexHomeConfig) (AdapterOutput, error) {
	timeoutContext, cancel := context.WithTimeout(ctx, adapter.Timeout)
	defer cancel()
	command := exec.CommandContext(timeoutContext, adapter.Command[0], adapter.Command[1:]...)
	command.Env = replaceEnvironment(os.Environ(), map[string]string{
		"CODEX_HOME": home.Path, "AGENT_BEACON_CODEX_HOME_ID": home.ID,
	})
	var stdout, stderr limitedBuffer
	stdout.limit = adapter.MaxStdoutBytes
	stderr.limit = 4096
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if timeoutContext.Err() != nil {
			return AdapterOutput{}, fmt.Errorf("adapter timed out after %s", adapter.Timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return AdapterOutput{}, fmt.Errorf("adapter failed: %w: %s", err, detail)
		}
		return AdapterOutput{}, fmt.Errorf("adapter failed: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var output AdapterOutput
	if err := decoder.Decode(&output); err != nil {
		return AdapterOutput{}, fmt.Errorf("decode adapter output: %w", err)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return AdapterOutput{}, errors.New("decode adapter output: multiple JSON values")
	}
	if output.SchemaVersion != 1 || output.HomeID != home.ID || output.ObservedAt.IsZero() ||
		output.Weekly.RemainingPercent < 0 || output.Weekly.RemainingPercent > 100 {
		return AdapterOutput{}, errors.New("adapter output failed schema validation")
	}
	if output.ResetCards.Available != nil && *output.ResetCards.Available < 0 {
		return AdapterOutput{}, errors.New("adapter reset card count is invalid")
	}
	if output.ResetCards.Available != nil && *output.ResetCards.Available == 0 && output.ResetCards.NearestExpiresAt != nil {
		return AdapterOutput{}, errors.New("adapter returned an expiry for zero reset cards")
	}
	return output, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if int64(buffer.Len()+len(data)) > buffer.limit {
		return 0, fmt.Errorf("output exceeds %d bytes", buffer.limit)
	}
	return buffer.Buffer.Write(data)
}

func replaceEnvironment(current []string, replacements map[string]string) []string {
	result := make([]string, 0, len(current)+len(replacements))
	for _, entry := range current {
		name := strings.SplitN(entry, "=", 2)[0]
		if _, replace := replacements[name]; !replace {
			result = append(result, entry)
		}
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+replacements[key])
	}
	return result
}

func quotaTransitionNotifications(runtime *homeRuntime, before, after protocol.CodexHome, now time.Time) []*protocol.Notification {
	var result []*protocol.Notification
	if before.WeeklyResetAt != nil && after.WeeklyResetAt != nil && !before.WeeklyResetAt.Equal(*after.WeeklyResetAt) &&
		after.WeeklyRemainingPercent > before.WeeklyRemainingPercent {
		for _, threshold := range []int{30, 15, 5} {
			runtime.thresholdArmed[threshold] = true
		}
		result = append(result, quotaNotification(after, now, "quota.weekly_reset", protocol.ThemeGreen,
			protocol.UrgencyNormal, 42, "reset", "Codex 一周额度已重置", 4000))
		return result
	}
	for _, threshold := range []struct {
		value       int
		kind, title string
		theme       protocol.Theme
		urgency     protocol.Urgency
		priority    uint8
		display     uint32
	}{
		{30, "quota.weekly_30", "Codex 一周额度偏低", protocol.ThemeYellow, protocol.UrgencyNormal, 45, 5000},
		{15, "quota.weekly_15", "Codex 一周额度需要关注", protocol.ThemeYellow, protocol.UrgencyAttention, 65, 6000},
		{5, "quota.weekly_5", "Codex 一周额度即将用尽", protocol.ThemeRed, protocol.UrgencyUrgent, 90, 8000},
	} {
		if !runtime.thresholdArmed[threshold.value] && after.WeeklyRemainingPercent >= threshold.value+5 {
			runtime.thresholdArmed[threshold.value] = true
		}
		if runtime.thresholdArmed[threshold.value] && before.WeeklyRemainingPercent > threshold.value && after.WeeklyRemainingPercent <= threshold.value {
			result = append(result, quotaNotification(after, now, threshold.kind, threshold.theme,
				threshold.urgency, threshold.priority, fmt.Sprintf("threshold:%d", threshold.value), threshold.title, threshold.display))
			runtime.thresholdArmed[threshold.value] = false
		}
	}
	return result
}

func quotaNotification(home protocol.CodexHome, now time.Time, kind string, theme protocol.Theme,
	urgency protocol.Urgency, priority uint8, episode, title string, display uint32) *protocol.Notification {
	window := "unknown"
	if home.WeeklyResetAt != nil {
		window = home.WeeklyResetAt.UTC().Format("20060102T150405Z")
	}
	return &protocol.Notification{
		Category: protocol.CategoryQuota, Kind: kind, Source: "codex", SubjectID: home.ID,
		Theme: theme, Urgency: urgency, Priority: priority,
		DedupeKey:    fmt.Sprintf("quota:%s:weekly:%s:%s", home.ID, window, episode),
		SupersedeKey: "quota:" + home.ID, Title: title,
		Detail: fmt.Sprintf("%s · 剩余 %d%%", home.Label, home.WeeklyRemainingPercent), SourceLabel: "Codex",
		DisplayMS: display, ExpiresAt: now.Add(10 * time.Minute), ReplayAfterInterrupt: urgency != protocol.UrgencyNormal, MaxReplays: 1,
	}
}

func quotaStaleNotification(home protocol.CodexHome, now time.Time) *protocol.Notification {
	return &protocol.Notification{
		Category: protocol.CategoryQuota, Kind: "quota.home_stale", Source: "codex", SubjectID: home.ID,
		Theme: protocol.ThemeYellow, Urgency: protocol.UrgencyAttention, Priority: 58,
		DedupeKey: "quota:" + home.ID + ":stale", SupersedeKey: "quota:" + home.ID,
		Title: "Codex 数据已过期", Detail: home.Label + " 暂不可用", SourceLabel: "Codex",
		DisplayMS: 6000, ExpiresAt: now.Add(15 * time.Minute), ReplayAfterInterrupt: true, MaxReplays: 1,
	}
}

func relayNotification(now time.Time, kind string, theme protocol.Theme, urgency protocol.Urgency,
	priority uint8, dedupe, title, detail string) *protocol.Notification {
	return &protocol.Notification{
		Category: protocol.CategorySystem, Kind: kind, Source: "relay", SubjectID: "relay",
		Theme: theme, Urgency: urgency, Priority: priority, DedupeKey: dedupe, SupersedeKey: "system:relay",
		Title: title, Detail: detail, SourceLabel: "0-0", DisplayMS: 6000,
		ExpiresAt: now.Add(30 * time.Minute), ReplayAfterInterrupt: urgency != protocol.UrgencyNormal, MaxReplays: 1,
	}
}

func sendUpdates(ctx context.Context, out chan<- providers.Update, updates []providers.Update) error {
	for _, update := range updates {
		select {
		case out <- update:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
