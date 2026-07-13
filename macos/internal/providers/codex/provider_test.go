package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers/relaybalance"
)

func TestAdapterHelperProcess(t *testing.T) {
	if os.Getenv("AGENT_BEACON_ADAPTER_HELPER") != "1" {
		return
	}
	available := 2
	reset := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	output := AdapterOutput{SchemaVersion: 1, HomeID: os.Getenv("AGENT_BEACON_CODEX_HOME_ID"), ObservedAt: time.Now().UTC()}
	output.Weekly.RemainingPercent = 18
	output.Weekly.ResetAt = &reset
	output.ResetCards.Available = &available
	output.ResetCards.NearestExpiresAt = &expires
	_ = json.NewEncoder(os.Stdout).Encode(output)
	os.Exit(0)
}

func TestRunAdapterSetsIndependentCodexHome(t *testing.T) {
	t.Setenv("AGENT_BEACON_ADAPTER_HELPER", "1")
	homePath := t.TempDir()
	output, err := runAdapter(context.Background(), config.CodexAdapterConfig{
		Command: []string{os.Args[0], "-test.run=TestAdapterHelperProcess"}, Timeout: 5 * time.Second, MaxStdoutBytes: 64 * 1024,
	}, config.CodexHomeConfig{ID: "main", Label: "MAIN", Path: homePath})
	if err != nil {
		t.Fatal(err)
	}
	if output.HomeID != "main" || output.Weekly.RemainingPercent != 18 || output.ResetCards.Available == nil || *output.ResetCards.Available != 2 {
		t.Fatalf("adapter output = %+v", output)
	}
}

func TestSelectWeeklyWindowIgnoresFiveHourWindow(t *testing.T) {
	fiveHours, week := int64(300), int64(10080)
	primary := &appServerWindow{UsedPercent: 70, WindowDurationMins: &fiveHours}
	secondary := &appServerWindow{UsedPercent: 82, WindowDurationMins: &week}
	selected, err := selectWeeklyWindow(appServerRateLimits{Primary: primary, Secondary: secondary})
	if err != nil || selected != secondary {
		t.Fatalf("selected = %+v, err = %v", selected, err)
	}
}

type fakeRelay struct{}

func (fakeRelay) Fetch(context.Context) (relaybalance.Result, error) {
	return relaybalance.Result{Remaining: 14.16, Unit: "USD", IsValid: true, FetchedAt: time.Now()}, nil
}

func TestSnapshotCombinesHomesAndRelayWithoutClobbering(t *testing.T) {
	t.Setenv("AGENT_BEACON_ADAPTER_HELPER", "1")
	provider := New(config.CodexProviderConfig{
		Enabled: true, RefreshInterval: time.Minute, StaleAfter: 3 * time.Minute,
		Homes:   []config.CodexHomeConfig{{ID: "main", Label: "MAIN", Path: t.TempDir()}, {ID: "vs", Label: "VS", Path: t.TempDir()}},
		Adapter: config.CodexAdapterConfig{Command: []string{os.Args[0], "-test.run=TestAdapterHelperProcess"}, Timeout: 5 * time.Second, MaxStdoutBytes: 64 * 1024},
	}, config.RelayBalanceConfig{Enabled: true, RefreshInterval: 5 * time.Minute, StaleAfter: 20 * time.Minute}, fakeRelay{})
	patch, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if patch.Codex == nil || len(patch.Codex.Homes) != 2 || patch.Codex.Relay.Remaining == nil || *patch.Codex.Relay.Remaining != 14.16 {
		t.Fatalf("combined snapshot = %+v", patch.Codex)
	}
}

func TestQuotaTransitionOnlyUsesWeeklyRemaining(t *testing.T) {
	reset := time.Now().Add(24 * time.Hour)
	before := protocol.CodexHome{ID: "main", Label: "MAIN", WeeklyRemainingPercent: 16, WeeklyResetAt: &reset}
	after := before
	after.WeeklyRemainingPercent = 14
	runtime := &homeRuntime{thresholdArmed: map[int]bool{30: true, 15: true, 5: true}}
	notifications := quotaTransitionNotifications(runtime, before, after, time.Now())
	if len(notifications) != 1 || notifications[0].Kind != "quota.weekly_15" || notifications[0].DedupeKey == "" {
		t.Fatalf("notifications = %+v", notifications)
	}
	if got := fmt.Sprint(notifications[0].Detail); got != "MAIN · 剩余 14%" {
		t.Fatalf("detail = %q", got)
	}
	before = after
	after.WeeklyRemainingPercent = 16
	if got := quotaTransitionNotifications(runtime, before, after, time.Now()); len(got) != 0 || runtime.thresholdArmed[15] {
		t.Fatalf("threshold rearmed before +5: %+v", got)
	}
	before = after
	after.WeeklyRemainingPercent = 20
	_ = quotaTransitionNotifications(runtime, before, after, time.Now())
	if !runtime.thresholdArmed[15] {
		t.Fatal("threshold did not rearm at +5")
	}
}
