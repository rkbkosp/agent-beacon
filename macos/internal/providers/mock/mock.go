package mock

import (
	"context"
	"fmt"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
)

var fixtureNames = []string{
	"codex-normal",
	"codex-one-home-stale",
	"codex-critical",
	"relay-14-16",
	"relay-invalid",
	"herdr-all-statuses",
	"herdr-blocked",
	"herdr-done",
	"weather-no-umbrella",
	"weather-lunch-umbrella",
	"weather-leave-umbrella",
	"weather-stale",
	"bridge-offline",
}

type Fixture struct {
	Patch        protocol.StatePatch
	Notification *protocol.Notification
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (*Provider) Name() string { return "mock" }

func (*Provider) Start(ctx context.Context, out chan<- providers.Update) error {
	snapshot := DefaultSnapshot(time.Now())
	update := providers.Update{Patch: protocol.StatePatch{
		Clock: &snapshot.Clock, Codex: &snapshot.Codex, Agents: &snapshot.Agents,
		Weather: &snapshot.Weather, System: &snapshot.System,
	}}
	select {
	case out <- update:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

func (*Provider) Snapshot(context.Context) (protocol.StatePatch, error) {
	snapshot := DefaultSnapshot(time.Now())
	return protocol.StatePatch{
		Clock: &snapshot.Clock, Codex: &snapshot.Codex, Agents: &snapshot.Agents,
		Weather: &snapshot.Weather, System: &snapshot.System,
	}, nil
}

func (*Provider) Health(context.Context) providers.Health {
	return providers.Health{Healthy: true, Detail: "mock provider ready"}
}

func Names() []string { return append([]string(nil), fixtureNames...) }

func DefaultSnapshot(now time.Time) protocol.Snapshot {
	resetMain := now.Add(21 * time.Hour)
	resetVS := now.Add(4 * 24 * time.Hour)
	expiresMain := now.Add(6 * 24 * time.Hour)
	expiresVS := now.Add(4 * 24 * time.Hour)
	cardsMain, cardsVS := 2, 1
	remaining := 14.16
	tokenRate := 42.7
	umbrella := true
	return protocol.Snapshot{
		Clock: protocol.ClockState{Timezone: "Asia/Shanghai", ServerTime: now},
		Codex: protocol.CodexState{
			Homes: []protocol.CodexHome{
				{ID: "main", Label: "MAIN", WeeklyRemainingPercent: 18, WeeklyResetAt: &resetMain,
					ResetCardsAvailable: &cardsMain, NearestResetCardExpiresAt: &expiresMain, UpdatedAt: now, Freshness: protocol.FreshnessFresh},
				{ID: "vs", Label: "VS", WeeklyRemainingPercent: 64, WeeklyResetAt: &resetVS,
					ResetCardsAvailable: &cardsVS, NearestResetCardExpiresAt: &expiresVS, UpdatedAt: now, Freshness: protocol.FreshnessFresh},
			},
			Relay: protocol.RelayState{Remaining: &remaining, Unit: "USD", IsValid: true, UpdatedAt: now, Freshness: protocol.FreshnessFresh},
			TokenRate: protocol.TokenRateState{TokensPerSecond: &tokenRate, ActiveSessions: 2, ActiveStreams: 3,
				WindowMS: 2000, Estimated: true, UpdatedAt: &now, Freshness: protocol.FreshnessFresh},
		},
		Agents: agentsAllStatuses(now),
		Weather: protocol.WeatherState{
			Location: "杭州", Provider: "qweather",
			Current:    protocol.WeatherCurrent{ObservedAt: now, TempC: 31, Icon: "101", Text: "多云", Freshness: protocol.FreshnessFresh},
			Lunch:      protocol.WeatherSlot{TargetAt: atHour(now, 12), IsPast: now.Hour() >= 12, TempC: 29, Icon: "305", Text: "小雨", POP: 60, PrecipMM: 0.5, Freshness: protocol.FreshnessCached},
			Leave:      protocol.WeatherSlot{TargetAt: atHour(now, 19), IsPast: now.Hour() >= 19, TempC: 27, Icon: "305", Text: "小雨", POP: 70, PrecipMM: 0.7, Freshness: protocol.FreshnessFresh},
			NextOuting: protocol.NextOuting{Slot: "leave", TargetAt: atHour(now, 19), UmbrellaRequired: &umbrella, Confidence: "high", Reason: "有雨"},
			UpdatedAt:  now,
		},
		System: protocol.SystemState{BridgeOnline: true, OverallFreshness: protocol.FreshnessFresh},
	}
}

func Build(name string, now time.Time) (Fixture, error) {
	base := DefaultSnapshot(now)
	switch name {
	case "codex-normal":
		return Fixture{Patch: protocol.StatePatch{Codex: &base.Codex}}, nil
	case "codex-one-home-stale":
		base.Codex.Homes = base.Codex.Homes[:1]
		base.Codex.Homes[0].Freshness = protocol.FreshnessStale
		return Fixture{Patch: protocol.StatePatch{Codex: &base.Codex}, Notification: notification(now,
			protocol.CategoryQuota, "quota.home_stale", "codex", "main", protocol.ThemeYellow,
			protocol.UrgencyAttention, 58, "quota:main:stale", "Codex 数据已过期", "MAIN 暂不可用", 6000, 15*time.Minute)}, nil
	case "codex-critical":
		base.Codex.Homes[0].WeeklyRemainingPercent = 4
		return Fixture{Patch: protocol.StatePatch{Codex: &base.Codex}, Notification: notification(now,
			protocol.CategoryQuota, "quota.weekly_5", "codex", "main", protocol.ThemeRed,
			protocol.UrgencyUrgent, 90, "quota:main:weekly:mock:threshold:5", "Codex 配额告急", "MAIN 剩余 4%", 8000, 10*time.Minute)}, nil
	case "relay-14-16":
		return Fixture{Patch: protocol.StatePatch{Codex: &base.Codex}}, nil
	case "relay-invalid":
		base.Codex.Relay.IsValid = false
		base.Codex.Relay.Remaining = nil
		return Fixture{Patch: protocol.StatePatch{Codex: &base.Codex}, Notification: notification(now,
			protocol.CategorySystem, "system.relay_key_invalid", "relay", "relay", protocol.ThemeYellow,
			protocol.UrgencyAttention, 70, "system:relay:key-invalid", "中转凭证无效", "0-0 API 凭证", 6000, 30*time.Minute)}, nil
	case "herdr-all-statuses":
		return Fixture{Patch: protocol.StatePatch{Agents: &base.Agents}}, nil
	case "herdr-blocked":
		base.Agents.CodexActive = false
		base.Agents.Items = []protocol.AgentItem{{PaneID: "w1:p1", Agent: "codex", DisplayName: "Chrome Plugin", Status: protocol.AgentBlocked, CustomStatus: "等待批准", Revision: 43}}
		return Fixture{Patch: protocol.StatePatch{Agents: &base.Agents}, Notification: notification(now,
			protocol.CategoryAgent, "agent.blocked", "herdr", "w1:p1", protocol.ThemeYellow,
			protocol.UrgencyAttention, 75, "agent:w1:p1:mock:blocked:43", "Agent 需要关注", "Chrome Plugin · 等待批准", 7000, 30*time.Minute)}, nil
	case "herdr-done":
		base.Agents.CodexActive = false
		base.Agents.Items = []protocol.AgentItem{{PaneID: "w1:p1", Agent: "codex", DisplayName: "Chrome Plugin", Status: protocol.AgentDone, Revision: 44}}
		return Fixture{Patch: protocol.StatePatch{Agents: &base.Agents}, Notification: notification(now,
			protocol.CategoryAgent, "agent.done", "herdr", "w1:p1", protocol.ThemeGreen,
			protocol.UrgencyNormal, 50, "agent:w1:p1:mock:done:44", "Agent 已完成", "Chrome Plugin · 已就绪", 4000, time.Minute)}, nil
	case "weather-no-umbrella":
		value := false
		base.Weather.NextOuting.UmbrellaRequired = &value
		base.Weather.NextOuting.Reason = "无雨"
		base.Weather.Leave = protocol.WeatherSlot{TargetAt: atHour(now, 19), TempC: 28, Icon: "100", Text: "晴", POP: 10, Freshness: protocol.FreshnessFresh}
		return Fixture{Patch: protocol.StatePatch{Weather: &base.Weather}}, nil
	case "weather-lunch-umbrella":
		base.Weather.NextOuting.Slot = "lunch"
		base.Weather.NextOuting.TargetAt = atHour(now, 12)
		base.Weather.NextOuting.Reason = "有雨"
		return Fixture{Patch: protocol.StatePatch{Weather: &base.Weather}, Notification: notification(now,
			protocol.CategoryWeather, "weather.umbrella_required", "qweather", "hangzhou:lunch", protocol.ThemeRed,
			protocol.UrgencyAttention, 72, "weather:hangzhou:lunch:mock:umbrella", "午饭记得带伞", "有雨", 6500, 60*time.Minute)}, nil
	case "weather-leave-umbrella":
		return Fixture{Patch: protocol.StatePatch{Weather: &base.Weather}, Notification: notification(now,
			protocol.CategoryWeather, "weather.umbrella_required", "qweather", "hangzhou:leave", protocol.ThemeRed,
			protocol.UrgencyAttention, 72, "weather:hangzhou:leave:mock:umbrella", "下班记得带伞", "有雨", 6500, 60*time.Minute)}, nil
	case "weather-stale":
		base.Weather.Current.Freshness = protocol.FreshnessStale
		base.Weather.Lunch.Freshness = protocol.FreshnessStale
		base.Weather.Leave.Freshness = protocol.FreshnessStale
		base.Weather.NextOuting.UmbrellaRequired = nil
		base.Weather.NextOuting.Confidence = "unknown"
		base.Weather.NextOuting.Reason = "数据不足"
		return Fixture{Patch: protocol.StatePatch{Weather: &base.Weather}, Notification: notification(now,
			protocol.CategorySystem, "system.weather_stale", "qweather", "hangzhou", protocol.ThemeYellow,
			protocol.UrgencyNormal, 40, "system:weather:hangzhou:stale", "天气数据不可用", "暂时无法判断是否带伞", 5000, 15*time.Minute)}, nil
	case "bridge-offline":
		base.System.BridgeOnline = false
		base.System.OverallFreshness = protocol.FreshnessStale
		return Fixture{Patch: protocol.StatePatch{System: &base.System}, Notification: notification(now,
			protocol.CategorySystem, "system.bridge_offline", "bridge", "bridge", protocol.ThemeYellow,
			protocol.UrgencyAttention, 78, "system:bridge:offline", "桥接服务离线", "Mac 连接已断开", 7000, 30*time.Minute)}, nil
	default:
		return Fixture{}, fmt.Errorf("unknown fixture %q", name)
	}
}

func agentsAllStatuses(now time.Time) protocol.AgentsState {
	return protocol.AgentsState{Provider: "herdr", Connected: true, CodexActive: true, UpdatedAt: now, Items: []protocol.AgentItem{
		{PaneID: "w1:p1", Agent: "codex", DisplayName: "Chrome Plugin", Status: protocol.AgentBlocked, CustomStatus: "等待批准", Revision: 42},
		{PaneID: "w1:p2", Agent: "codex", DisplayName: "CaseForge", Status: protocol.AgentWorking, Focused: true, Revision: 18,
			AgentSession: &protocol.AgentSession{Source: "herdr:codex", Kind: "id", Value: "session-working"}},
		{PaneID: "w1:p3", DisplayName: "Docs", Status: protocol.AgentDone, Revision: 9},
		{PaneID: "w1:p4", DisplayName: "Review", Status: protocol.AgentIdle, Revision: 3},
		{PaneID: "w1:p5", DisplayName: "未知窗格", Status: protocol.AgentUnknown, Revision: 1},
	}}
}

func notification(now time.Time, category protocol.Category, kind, source, subject string, theme protocol.Theme,
	urgency protocol.Urgency, priority uint8, dedupe, title, detail string, displayMS uint32, ttl time.Duration) *protocol.Notification {
	return &protocol.Notification{
		Category: category, Kind: kind, Source: source, SubjectID: subject, Theme: theme, Urgency: urgency,
		Priority: priority, DedupeKey: dedupe, SupersedeKey: string(category) + ":" + subject,
		Title: title, Detail: detail, SourceLabel: source, DisplayMS: displayMS, ExpiresAt: now.Add(ttl),
		ReplayAfterInterrupt: urgency != protocol.UrgencyNormal, MaxReplays: 1,
	}
}

func atHour(now time.Time, hour int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
}
