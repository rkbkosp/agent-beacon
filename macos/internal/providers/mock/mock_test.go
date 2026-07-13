package mock

import (
	"context"
	"testing"
	"time"
)

func TestProviderSnapshotContainsOnlyCurrentThreePageDomains(t *testing.T) {
	provider := New()
	if health := provider.Health(context.Background()); !health.Healthy {
		t.Fatalf("health = %+v", health)
	}
	snapshot := DefaultSnapshot(time.Date(2026, 7, 14, 14, 30, 0, 0, time.FixedZone("CST", 8*60*60)))
	if len(snapshot.Codex.Homes) != 2 || snapshot.Codex.Relay.Remaining == nil || *snapshot.Codex.Relay.Remaining != 14.16 {
		t.Fatalf("codex snapshot = %+v", snapshot.Codex)
	}
	if snapshot.Agents.Provider != "herdr" || snapshot.Weather.Provider != "qweather" {
		t.Fatalf("providers = agents:%q weather:%q", snapshot.Agents.Provider, snapshot.Weather.Provider)
	}
}

func TestAllDocumentedFixturesAreAvailable(t *testing.T) {
	want := []string{
		"codex-normal", "codex-one-home-stale", "codex-critical", "relay-14-16", "relay-invalid",
		"herdr-all-statuses", "herdr-blocked", "herdr-done", "weather-no-umbrella",
		"weather-lunch-umbrella", "weather-leave-umbrella", "weather-stale", "bridge-offline",
	}
	if got := Names(); len(got) != len(want) {
		t.Fatalf("fixture names = %v", got)
	}
	for _, name := range want {
		fixture, err := Build(name, time.Date(2026, 7, 14, 14, 30, 0, 0, time.FixedZone("CST", 8*60*60)))
		if err != nil {
			t.Fatalf("Build(%q): %v", name, err)
		}
		if fixture.Patch.Clock == nil && fixture.Patch.Codex == nil && fixture.Patch.Agents == nil &&
			fixture.Patch.Weather == nil && fixture.Patch.System == nil {
			t.Fatalf("Build(%q) returned empty patch", name)
		}
		if name == "herdr-blocked" && (fixture.Notification == nil || fixture.Notification.Kind != "agent.blocked") {
			t.Fatalf("blocked fixture = %+v", fixture)
		}
		if name == "herdr-done" && (fixture.Notification == nil || fixture.Notification.Kind != "agent.done") {
			t.Fatalf("done fixture = %+v", fixture)
		}
		if name == "weather-leave-umbrella" && (fixture.Notification == nil || fixture.Notification.Kind != "weather.umbrella_required") {
			t.Fatalf("weather fixture = %+v", fixture)
		}
	}
}

func TestUnknownFixtureIsRejected(t *testing.T) {
	if _, err := Build("message-new", time.Now()); err == nil {
		t.Fatal("unknown or forbidden fixture must fail")
	}
}
