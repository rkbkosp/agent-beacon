package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
)

func TestReadTokenRateStateValidatesDaemonContractAndFreshness(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "token-rate.json")
	writeTokenRateState(t, path, now, 42.7, 2, 3)

	state, err := readTokenRateState(path, now.Add(time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state.TokensPerSecond == nil || *state.TokensPerSecond != 42.7 || state.ActiveSessions != 2 ||
		state.ActiveStreams != 3 || state.WindowMS != 2000 || state.Freshness != protocol.FreshnessFresh {
		t.Fatalf("fresh state = %+v", state)
	}

	state, err = readTokenRateState(path, now.Add(3*time.Second), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state.TokensPerSecond != nil || state.ActiveSessions != 0 || state.ActiveStreams != 0 ||
		state.Freshness != protocol.FreshnessStale {
		t.Fatalf("stale state = %+v", state)
	}
}

func TestReadTokenRateStateRejectsUnknownContractAndLoosePermissions(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "token-rate.json")
	writeTokenRateState(t, path, now, 10, 1, 1)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(string(data[:len(data)-2]) + `,"extra":true}` + "\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenRateState(path, now, time.Second); err == nil {
		t.Fatal("unknown daemon state field must be rejected")
	}
	writeTokenRateState(t, path, now, 10, 1, 1)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenRateState(path, now, time.Second); err == nil {
		t.Fatal("group-readable daemon state must be rejected")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenRateState(path, now, time.Second); err == nil {
		t.Fatal("owner-executable daemon state must be rejected")
	}
}

func TestProviderPublishesOnlyVisibleTokenRateChangesAndExpiresMissingFile(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "token-rate.json")
	writeTokenRateState(t, path, now, 42.7, 2, 3)
	provider := New(config.CodexProviderConfig{
		Homes: []config.CodexHomeConfig{{ID: "main", Label: "MAIN", Path: t.TempDir()}},
		TokenRate: config.CodexTokenRateConfig{Enabled: true, StateFile: path,
			RefreshInterval: 200 * time.Millisecond, StaleAfter: 2 * time.Second},
	}, config.RelayBalanceConfig{}, nil)
	provider.now = func() time.Time { return now }

	updates, err := provider.refreshTokenRate()
	if err != nil || len(updates) != 1 {
		t.Fatalf("first refresh updates=%d err=%v", len(updates), err)
	}
	updates, err = provider.refreshTokenRate()
	if err != nil || len(updates) != 0 {
		t.Fatalf("unchanged refresh updates=%d err=%v", len(updates), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	updates, err = provider.refreshTokenRate()
	if err == nil || len(updates) != 1 || provider.tokenRateState.Freshness != protocol.FreshnessCached {
		t.Fatalf("cached refresh state=%+v updates=%d err=%v", provider.tokenRateState, len(updates), err)
	}
	now = now.Add(2 * time.Second)
	updates, err = provider.refreshTokenRate()
	if err == nil || len(updates) != 1 || provider.tokenRateState.TokensPerSecond != nil ||
		provider.tokenRateState.Freshness != protocol.FreshnessStale {
		t.Fatalf("stale refresh state=%+v updates=%d err=%v", provider.tokenRateState, len(updates), err)
	}
}

func writeTokenRateState(t *testing.T, path string, updatedAt time.Time, rate float64, sessions, streams int) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"version":1,"metric":"visible_output_tokens_per_second","estimated":true,`+
			`"tokens_per_second":%.1f,"raw_tokens_per_second":%.1f,`+
			`"active_sessions":%d,"active_streams":%d,"window_ms":2000,"updated_at_unix_ms":%d}`+"\n",
		rate, rate, sessions, streams, updatedAt.UnixMilli())
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
}
