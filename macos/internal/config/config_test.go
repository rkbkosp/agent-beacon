package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaultsAndOverrides(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "secret-from-env")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  listen: "127.0.0.1:9999"
  max_request_bytes: 131072
notifications:
  dedupe_window: 90s
providers:
  mock:
    enabled: false
  herdr:
    enabled: true
    session: work
    socket_path: /tmp/herdr-work.sock
    reconnect_max: 12s
    full_resync_interval: 45s
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server.Listen != "127.0.0.1:9999" || got.Server.DeviceSendQueue != 64 || got.Server.MaxRequestBytes != 131072 {
		t.Fatalf("server config = %+v", got.Server)
	}
	if got.Token != "secret-from-env" {
		t.Fatalf("token was not loaded from environment")
	}
	if got.Notifications.QueueCapacity != 16 || got.Notifications.DedupeWindow != 90*time.Second {
		t.Fatalf("notification config = %+v", got.Notifications)
	}
	if got.Providers.Mock.Enabled {
		t.Fatal("mock provider override was ignored")
	}
	if !got.Providers.Herdr.Enabled || got.Providers.Herdr.Session != "work" ||
		got.Providers.Herdr.SocketPath != "/tmp/herdr-work.sock" ||
		got.Providers.Herdr.ReconnectMax != 12*time.Second ||
		got.Providers.Herdr.FullResyncInterval != 45*time.Second {
		t.Fatalf("herdr config = %+v", got.Providers.Herdr)
	}
}

func TestLoadRequiresTokenFromEnvironment(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: 127.0.0.1:8787\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("missing AGENT_BEACON_TOKEN must fail")
	}
}

func TestLoadCodexRelayAndTokenFile(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenPath := filepath.Join(home, "token")
	if err := os.WriteFile(tokenPath, []byte("token-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
server:
  token_file: "~/token"
providers:
  codex:
    enabled: true
    refresh_interval: 1m
    stale_after: 3m
    homes:
      - id: main
        label: MAIN
        path: ~/.codex
      - id: vs
        label: VS
        path: ~/.codex-vs
    adapter:
      command: ["/usr/bin/true"]
      timeout: 10s
      max_stdout_bytes: 65536
    token_rate:
      enabled: true
      socket_path: "~/token-rate.sock"
      state_file: "~/token-rate.json"
      refresh_interval: 200ms
      stale_after: 2s
  relay_balance:
    enabled: true
    endpoint: https://api.0-0.pro/v1/usage
    secret_name: zero-api-key
    refresh_interval: 5m
    timeout: 5s
    stale_after: 20m
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "token-from-file" || len(got.Providers.Codex.Homes) != 2 || !got.Providers.RelayBalance.Enabled {
		t.Fatalf("config = %+v", got)
	}
	if got.Providers.Codex.Homes[0].Path != filepath.Join(home, ".codex") {
		t.Fatalf("home path = %q", got.Providers.Codex.Homes[0].Path)
	}
	if !got.Providers.Codex.TokenRate.Enabled || got.Providers.Codex.TokenRate.SocketPath != filepath.Join(home, "token-rate.sock") ||
		got.Providers.Codex.TokenRate.StateFile != filepath.Join(home, "token-rate.json") ||
		got.Providers.Codex.TokenRate.RefreshInterval != 200*time.Millisecond || got.Providers.Codex.TokenRate.StaleAfter != 2*time.Second {
		t.Fatalf("token-rate config = %+v", got.Providers.Codex.TokenRate)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown config key to fail")
	}
}

func TestLoadRejectsTokenRateWithoutCodexProvider(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "bridge-secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
providers:
  codex:
    enabled: false
    token_rate:
      enabled: true
      socket_path: /tmp/codex-token-rate.sock
      state_file: /tmp/codex-token-rate.json
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "requires providers.codex.enabled") {
		t.Fatalf("expected token-rate dependency error, got %v", err)
	}
}

func TestLoadWeatherConfigExpandsTildeAndParsesDurations(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "bridge-secret")
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
providers:
  weather:
    enabled: true
    provider: qweather
    api_host: "abc1234xyz.def.qweatherapi.com"
    project_id: "PROJECT123"
    credential_id: "CREDENTIAL456"
    private_key_path: "~/.weather/ed25519-private.pem"
    public_key_path: "~/.weather/ed25519-public.pem"
    location: "120.16,30.27"
    location_label: "杭州"
    timezone: "Asia/Shanghai"
    lang: "zh"
    schedule:
      lunch: "12:00"
      leave: "19:00"
      active_weekdays: [1, 2, 3, 4, 5]
    refresh:
      now: 10m
      hourly: 30m
      request_timeout: 8s
      force_before_outing: [60m, 30m]
    cache:
      now_stale_after: 45m
      hourly_stale_after: 90m
      persist_last_good: true
    umbrella:
      window_before: 60m
      window_after: 60m
      pop_threshold: 40
      repeat_before_outing: 30m
    satellite_radiation:
      enabled: true
      latitude: 30.2163
      longitude: 120.1734
      lunch_refresh: "11:57"
      leave_refresh: "18:28"
      stale_after: 75m
      direct_required: 300
      ghi_required: 550
      required_direct_share: 0.35
      direct_suggested: 150
      ghi_suggested: 400
      suggested_direct_share: 0.25
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	weather := got.Providers.Weather
	if !weather.Enabled || weather.ValidationError != "" || weather.Provider != "qweather" {
		t.Fatalf("weather enablement = %+v", weather)
	}
	if weather.APIHost != "abc1234xyz.def.qweatherapi.com" || weather.ProjectID != "PROJECT123" || weather.CredentialID != "CREDENTIAL456" {
		t.Fatalf("weather identity config = %+v", weather)
	}
	wantPrivate := filepath.Join(os.Getenv("HOME"), ".weather", "ed25519-private.pem")
	wantPublic := filepath.Join(os.Getenv("HOME"), ".weather", "ed25519-public.pem")
	if weather.PrivateKeyPath != wantPrivate || weather.PublicKeyPath != wantPublic {
		t.Fatalf("expanded paths = %q, %q", weather.PrivateKeyPath, weather.PublicKeyPath)
	}
	if weather.Location != "120.16,30.27" || weather.LocationLabel != "杭州" || weather.Timezone != "Asia/Shanghai" || weather.Lang != "zh" {
		t.Fatalf("weather locale config = %+v", weather)
	}
	if weather.Schedule.Lunch != "12:00" || weather.Schedule.Leave != "19:00" || len(weather.Schedule.ActiveWeekdays) != 5 {
		t.Fatalf("weather schedule = %+v", weather.Schedule)
	}
	if weather.Refresh.Now != 10*time.Minute || weather.Refresh.Hourly != 30*time.Minute || weather.Refresh.RequestTimeout != 8*time.Second {
		t.Fatalf("weather refresh = %+v", weather.Refresh)
	}
	if len(weather.Refresh.ForceBeforeOuting) != 2 || weather.Refresh.ForceBeforeOuting[0] != time.Hour || weather.Refresh.ForceBeforeOuting[1] != 30*time.Minute {
		t.Fatalf("force refresh schedule = %v", weather.Refresh.ForceBeforeOuting)
	}
	if weather.Cache.NowStaleAfter != 45*time.Minute || weather.Cache.HourlyStaleAfter != 90*time.Minute || !weather.Cache.PersistLastGood {
		t.Fatalf("weather cache = %+v", weather.Cache)
	}
	if weather.Umbrella.WindowBefore != time.Hour || weather.Umbrella.WindowAfter != time.Hour || weather.Umbrella.POPThreshold != 40 || weather.Umbrella.RepeatBeforeOuting != 30*time.Minute {
		t.Fatalf("weather umbrella = %+v", weather.Umbrella)
	}
	if !weather.Satellite.Enabled || weather.Satellite.Latitude != 30.2163 || weather.Satellite.Longitude != 120.1734 ||
		weather.Satellite.LunchRefresh != "11:57" || weather.Satellite.LeaveRefresh != "18:28" ||
		weather.Satellite.StaleAfter != 75*time.Minute || weather.Satellite.DirectRequired != 300 ||
		weather.Satellite.GHIRequired != 550 || weather.Satellite.RequiredDirectShare != 0.35 ||
		weather.Satellite.DirectSuggested != 150 || weather.Satellite.GHISuggested != 400 ||
		weather.Satellite.SuggestedDirectShare != 0.25 {
		t.Fatalf("weather satellite radiation = %+v", weather.Satellite)
	}
}

func TestInvalidWeatherConfigDisablesOnlyWeather(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "bridge-secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
providers:
  weather:
    enabled: true
    provider: qweather
    api_host: "https://api.qweather.com/v7"
    project_id: "project"
    credential_id: "credential"
    private_key_path: "~/.weather/key.pem"
    location: "120.16,30.27"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("weather validation must not stop bridge config loading: %v", err)
	}
	if got.Providers.Weather.Enabled {
		t.Fatal("invalid weather config must be disabled")
	}
	if !strings.Contains(got.Providers.Weather.ValidationError, "providers.weather.api_host") {
		t.Fatalf("validation error = %q", got.Providers.Weather.ValidationError)
	}
	if got.Providers.Mock.Enabled {
		t.Fatal("invalid weather configuration must not enable Mock fixtures")
	}
}

func TestInvalidSatelliteRadiationConfigDisablesOnlyWeather(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "bridge-secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
providers:
  weather:
    enabled: true
    provider: qweather
    api_host: abc.qweatherapi.com
    project_id: project
    credential_id: credential
    private_key_path: /tmp/key.pem
    location: "120.16,30.27"
    location_label: "杭州"
    satellite_radiation:
      enabled: true
      latitude: 120
      longitude: 30
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers.Weather.Enabled || !strings.Contains(got.Providers.Weather.ValidationError, "satellite_radiation") {
		t.Fatalf("invalid satellite config = %+v", got.Providers.Weather)
	}
}

func TestCheckedInExampleConfigLoadsWithStrictFields(t *testing.T) {
	t.Setenv("AGENT_BEACON_TOKEN", "test-token")
	got, err := Load("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got.Providers.Weather.Enabled || got.Providers.Weather.Provider != "qweather" {
		t.Fatalf("example weather config = %+v", got.Providers.Weather)
	}
	if !got.Providers.Herdr.Enabled {
		t.Fatal("example Herdr provider unexpectedly disabled")
	}
}
