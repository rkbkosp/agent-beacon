package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Listen          string
	DeviceSendQueue int
	MaxRequestBytes int64
	TokenFile       string
}

type NotificationsConfig struct {
	QueueCapacity int
	DedupeWindow  time.Duration
}

type MockProviderConfig struct {
	Enabled bool
}

type CodexHomeConfig struct {
	ID    string
	Label string
	Path  string
}

type CodexAdapterConfig struct {
	Command        []string
	Timeout        time.Duration
	MaxStdoutBytes int64
}

type CodexProviderConfig struct {
	Enabled         bool
	RefreshInterval time.Duration
	StaleAfter      time.Duration
	Homes           []CodexHomeConfig
	Adapter         CodexAdapterConfig
}

type RelayBalanceConfig struct {
	Enabled         bool
	Endpoint        string
	SecretName      string
	RefreshInterval time.Duration
	Timeout         time.Duration
	StaleAfter      time.Duration
}

type HerdrProviderConfig struct {
	Enabled            bool
	Session            string
	SocketPath         string
	ReconnectMax       time.Duration
	FullResyncInterval time.Duration
}

type WeatherScheduleConfig struct {
	Lunch          string
	Leave          string
	ActiveWeekdays []int
}

type WeatherRefreshConfig struct {
	Now               time.Duration
	Hourly            time.Duration
	RequestTimeout    time.Duration
	ForceBeforeOuting []time.Duration
}

type WeatherCacheConfig struct {
	NowStaleAfter    time.Duration
	HourlyStaleAfter time.Duration
	PersistLastGood  bool
}

type WeatherUmbrellaConfig struct {
	WindowBefore       time.Duration
	WindowAfter        time.Duration
	POPThreshold       int
	RepeatBeforeOuting time.Duration
}

type SatelliteRadiationConfig struct {
	Enabled              bool
	Latitude             float64
	Longitude            float64
	LunchRefresh         string
	LeaveRefresh         string
	StaleAfter           time.Duration
	DirectRequired       float64
	GHIRequired          float64
	RequiredDirectShare  float64
	DirectSuggested      float64
	GHISuggested         float64
	SuggestedDirectShare float64
}

type WeatherConfig struct {
	Enabled         bool
	Provider        string
	APIHost         string
	ProjectID       string
	CredentialID    string
	PrivateKeyPath  string
	PublicKeyPath   string
	Location        string
	LocationLabel   string
	Timezone        string
	Lang            string
	Schedule        WeatherScheduleConfig
	Refresh         WeatherRefreshConfig
	Cache           WeatherCacheConfig
	Umbrella        WeatherUmbrellaConfig
	Satellite       SatelliteRadiationConfig
	ValidationError string
}

type ProvidersConfig struct {
	Mock         MockProviderConfig
	Codex        CodexProviderConfig
	RelayBalance RelayBalanceConfig
	Herdr        HerdrProviderConfig
	Weather      WeatherConfig
}

type Config struct {
	Server        ServerConfig
	Notifications NotificationsConfig
	Providers     ProvidersConfig
	Token         string
}

type rawConfig struct {
	Server struct {
		Listen          string `yaml:"listen"`
		DeviceSendQueue int    `yaml:"device_send_queue"`
		MaxRequestBytes int64  `yaml:"max_request_bytes"`
		TokenFile       string `yaml:"token_file"`
	} `yaml:"server"`
	Notifications struct {
		QueueCapacity int    `yaml:"queue_capacity"`
		DedupeWindow  string `yaml:"dedupe_window"`
	} `yaml:"notifications"`
	Providers struct {
		Mock struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"mock"`
		Codex struct {
			Enabled         *bool  `yaml:"enabled"`
			RefreshInterval string `yaml:"refresh_interval"`
			StaleAfter      string `yaml:"stale_after"`
			Homes           []struct {
				ID    string `yaml:"id"`
				Label string `yaml:"label"`
				Path  string `yaml:"path"`
			} `yaml:"homes"`
			Adapter struct {
				Command        []string `yaml:"command"`
				Timeout        string   `yaml:"timeout"`
				MaxStdoutBytes int64    `yaml:"max_stdout_bytes"`
			} `yaml:"adapter"`
		} `yaml:"codex"`
		RelayBalance struct {
			Enabled         *bool  `yaml:"enabled"`
			Endpoint        string `yaml:"endpoint"`
			SecretName      string `yaml:"secret_name"`
			RefreshInterval string `yaml:"refresh_interval"`
			Timeout         string `yaml:"timeout"`
			StaleAfter      string `yaml:"stale_after"`
		} `yaml:"relay_balance"`
		Herdr struct {
			Enabled            *bool  `yaml:"enabled"`
			Session            string `yaml:"session"`
			SocketPath         string `yaml:"socket_path"`
			ReconnectMax       string `yaml:"reconnect_max"`
			FullResyncInterval string `yaml:"full_resync_interval"`
		} `yaml:"herdr"`
		Weather struct {
			Enabled        *bool  `yaml:"enabled"`
			Provider       string `yaml:"provider"`
			APIHost        string `yaml:"api_host"`
			ProjectID      string `yaml:"project_id"`
			CredentialID   string `yaml:"credential_id"`
			PrivateKeyPath string `yaml:"private_key_path"`
			PublicKeyPath  string `yaml:"public_key_path"`
			Location       string `yaml:"location"`
			LocationLabel  string `yaml:"location_label"`
			Timezone       string `yaml:"timezone"`
			Lang           string `yaml:"lang"`
			Schedule       struct {
				Lunch          string `yaml:"lunch"`
				Leave          string `yaml:"leave"`
				ActiveWeekdays []int  `yaml:"active_weekdays"`
			} `yaml:"schedule"`
			Refresh struct {
				Now               string   `yaml:"now"`
				Hourly            string   `yaml:"hourly"`
				RequestTimeout    string   `yaml:"request_timeout"`
				ForceBeforeOuting []string `yaml:"force_before_outing"`
			} `yaml:"refresh"`
			Cache struct {
				NowStaleAfter    string `yaml:"now_stale_after"`
				HourlyStaleAfter string `yaml:"hourly_stale_after"`
				PersistLastGood  *bool  `yaml:"persist_last_good"`
			} `yaml:"cache"`
			Umbrella struct {
				WindowBefore       string `yaml:"window_before"`
				WindowAfter        string `yaml:"window_after"`
				POPThreshold       *int   `yaml:"pop_threshold"`
				RepeatBeforeOuting string `yaml:"repeat_before_outing"`
			} `yaml:"umbrella"`
			Satellite struct {
				Enabled              *bool    `yaml:"enabled"`
				Latitude             *float64 `yaml:"latitude"`
				Longitude            *float64 `yaml:"longitude"`
				LunchRefresh         string   `yaml:"lunch_refresh"`
				LeaveRefresh         string   `yaml:"leave_refresh"`
				StaleAfter           string   `yaml:"stale_after"`
				DirectRequired       *float64 `yaml:"direct_required"`
				GHIRequired          *float64 `yaml:"ghi_required"`
				RequiredDirectShare  *float64 `yaml:"required_direct_share"`
				DirectSuggested      *float64 `yaml:"direct_suggested"`
				GHISuggested         *float64 `yaml:"ghi_suggested"`
				SuggestedDirectShare *float64 `yaml:"suggested_direct_share"`
			} `yaml:"satellite_radiation"`
		} `yaml:"weather"`
	} `yaml:"providers"`
}

func Default() Config {
	return Config{
		Server:        ServerConfig{Listen: "0.0.0.0:8787", DeviceSendQueue: 64, MaxRequestBytes: 256 * 1024},
		Notifications: NotificationsConfig{QueueCapacity: 16, DedupeWindow: time.Minute},
		Providers: ProvidersConfig{
			Mock: MockProviderConfig{Enabled: false},
			Codex: CodexProviderConfig{
				RefreshInterval: time.Minute, StaleAfter: 3 * time.Minute,
				Adapter: CodexAdapterConfig{Timeout: 10 * time.Second, MaxStdoutBytes: 64 * 1024},
			},
			RelayBalance: RelayBalanceConfig{
				Endpoint: "https://api.0-0.pro/v1/usage", SecretName: "zero-api-key",
				RefreshInterval: 5 * time.Minute, Timeout: 5 * time.Second, StaleAfter: 20 * time.Minute,
			},
			Herdr: HerdrProviderConfig{
				Session: "default", ReconnectMax: 30 * time.Second,
				FullResyncInterval: 60 * time.Second,
			},
			Weather: WeatherConfig{
				Provider: "qweather", Timezone: "Asia/Shanghai", Lang: "zh",
				Schedule: WeatherScheduleConfig{Lunch: "12:00", Leave: "19:00", ActiveWeekdays: []int{1, 2, 3, 4, 5}},
				Refresh: WeatherRefreshConfig{Now: 10 * time.Minute, Hourly: 30 * time.Minute, RequestTimeout: 8 * time.Second,
					ForceBeforeOuting: []time.Duration{time.Hour, 30 * time.Minute}},
				Cache:    WeatherCacheConfig{NowStaleAfter: 45 * time.Minute, HourlyStaleAfter: 90 * time.Minute, PersistLastGood: true},
				Umbrella: WeatherUmbrellaConfig{WindowBefore: time.Hour, WindowAfter: time.Hour, POPThreshold: 40, RepeatBeforeOuting: 30 * time.Minute},
				Satellite: SatelliteRadiationConfig{
					Latitude: 30.2163, Longitude: 120.1734, LunchRefresh: "11:57", LeaveRefresh: "18:28",
					StaleAfter: 75 * time.Minute, DirectRequired: 300, GHIRequired: 550, RequiredDirectShare: 0.35,
					DirectSuggested: 150, GHISuggested: 400, SuggestedDirectShare: 0.25,
				},
			},
		},
	}
}

func Load(path string) (Config, error) {
	return load(path, true)
}

func LoadProviders(path string) (Config, error) {
	return load(path, false)
}

func load(path string, requireToken bool) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var raw rawConfig
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	result := Default()
	if raw.Server.Listen != "" {
		result.Server.Listen = raw.Server.Listen
	}
	if raw.Server.DeviceSendQueue != 0 {
		result.Server.DeviceSendQueue = raw.Server.DeviceSendQueue
	}
	if raw.Server.MaxRequestBytes != 0 {
		result.Server.MaxRequestBytes = raw.Server.MaxRequestBytes
	}
	result.Server.TokenFile = expandUserPath(raw.Server.TokenFile)
	if raw.Notifications.QueueCapacity != 0 {
		result.Notifications.QueueCapacity = raw.Notifications.QueueCapacity
	}
	if raw.Notifications.DedupeWindow != "" {
		value, err := time.ParseDuration(raw.Notifications.DedupeWindow)
		if err != nil {
			return Config{}, fmt.Errorf("parse notifications.dedupe_window: %w", err)
		}
		result.Notifications.DedupeWindow = value
	}
	if raw.Providers.Mock.Enabled != nil {
		result.Providers.Mock.Enabled = *raw.Providers.Mock.Enabled
	}
	if err := applyRawCodex(&result.Providers.Codex, &raw); err != nil {
		return Config{}, err
	}
	if err := applyRawRelayBalance(&result.Providers.RelayBalance, &raw); err != nil {
		return Config{}, err
	}
	if result.Providers.RelayBalance.Enabled && !result.Providers.Codex.Enabled {
		return Config{}, fmt.Errorf("providers.relay_balance requires providers.codex because both publish the Codex section")
	}
	if raw.Providers.Herdr.Enabled != nil {
		result.Providers.Herdr.Enabled = *raw.Providers.Herdr.Enabled
	}
	assignNonEmpty(&result.Providers.Herdr.Session, strings.TrimSpace(raw.Providers.Herdr.Session))
	result.Providers.Herdr.SocketPath = expandUserPath(raw.Providers.Herdr.SocketPath)
	if raw.Providers.Herdr.ReconnectMax != "" {
		value, durationErr := time.ParseDuration(raw.Providers.Herdr.ReconnectMax)
		if durationErr != nil || value <= 0 {
			return Config{}, fmt.Errorf("providers.herdr.reconnect_max must be a positive duration")
		}
		result.Providers.Herdr.ReconnectMax = value
	}
	if raw.Providers.Herdr.FullResyncInterval != "" {
		value, durationErr := time.ParseDuration(raw.Providers.Herdr.FullResyncInterval)
		if durationErr != nil || value <= 0 {
			return Config{}, fmt.Errorf("providers.herdr.full_resync_interval must be a positive duration")
		}
		result.Providers.Herdr.FullResyncInterval = value
	}
	applyRawWeather(&result.Providers.Weather, &raw)
	if result.Providers.Weather.Enabled {
		if err := ValidateWeather(result.Providers.Weather); err != nil {
			result.Providers.Weather.Enabled = false
			result.Providers.Weather.ValidationError = err.Error()
		}
	}
	result.Token = os.Getenv("AGENT_BEACON_TOKEN")
	if result.Token == "" && result.Server.TokenFile != "" {
		data, tokenErr := os.ReadFile(result.Server.TokenFile)
		if tokenErr != nil && requireToken {
			return Config{}, fmt.Errorf("read server.token_file: %w", tokenErr)
		}
		result.Token = strings.TrimSpace(string(data))
	}
	if requireToken && result.Token == "" {
		return Config{}, fmt.Errorf("AGENT_BEACON_TOKEN is required")
	}
	return result, nil
}

func applyRawCodex(codex *CodexProviderConfig, raw *rawConfig) error {
	value := &raw.Providers.Codex
	if value.Enabled != nil {
		codex.Enabled = *value.Enabled
	}
	if value.RefreshInterval != "" {
		parsed, err := positiveDuration("providers.codex.refresh_interval", value.RefreshInterval)
		if err != nil {
			return err
		}
		codex.RefreshInterval = parsed
	}
	if value.StaleAfter != "" {
		parsed, err := positiveDuration("providers.codex.stale_after", value.StaleAfter)
		if err != nil {
			return err
		}
		codex.StaleAfter = parsed
	}
	if value.Adapter.Timeout != "" {
		parsed, err := positiveDuration("providers.codex.adapter.timeout", value.Adapter.Timeout)
		if err != nil {
			return err
		}
		codex.Adapter.Timeout = parsed
	}
	if value.Adapter.MaxStdoutBytes != 0 {
		codex.Adapter.MaxStdoutBytes = value.Adapter.MaxStdoutBytes
	}
	if value.Adapter.Command != nil {
		codex.Adapter.Command = append([]string(nil), value.Adapter.Command...)
		if len(codex.Adapter.Command) > 0 && codex.Adapter.Command[0] == "@bridge" {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve providers.codex.adapter.command @bridge: %w", err)
			}
			codex.Adapter.Command[0] = executable
		}
	}
	if value.Homes != nil {
		codex.Homes = make([]CodexHomeConfig, 0, len(value.Homes))
		for _, home := range value.Homes {
			codex.Homes = append(codex.Homes, CodexHomeConfig{
				ID: strings.TrimSpace(home.ID), Label: strings.TrimSpace(home.Label), Path: expandUserPath(home.Path),
			})
		}
	}
	if !codex.Enabled {
		return nil
	}
	if len(codex.Homes) < 1 || len(codex.Homes) > 2 {
		return fmt.Errorf("providers.codex.homes must contain one or two homes")
	}
	seen := make(map[string]bool, len(codex.Homes))
	for _, home := range codex.Homes {
		if home.ID == "" || home.Label == "" || home.Path == "" {
			return fmt.Errorf("providers.codex.homes id, label, and path are required")
		}
		if seen[home.ID] {
			return fmt.Errorf("providers.codex.homes id %q is duplicated", home.ID)
		}
		seen[home.ID] = true
		if !filepath.IsAbs(home.Path) {
			return fmt.Errorf("providers.codex home %q path must resolve to an absolute path", home.ID)
		}
		if len(home.Label) > 8 {
			return fmt.Errorf("providers.codex home %q label must be at most 8 bytes", home.ID)
		}
	}
	if len(codex.Adapter.Command) == 0 || !filepath.IsAbs(codex.Adapter.Command[0]) {
		return fmt.Errorf("providers.codex.adapter.command must start with an absolute executable path or @bridge")
	}
	if codex.Adapter.MaxStdoutBytes < 1024 || codex.Adapter.MaxStdoutBytes > 4*1024*1024 {
		return fmt.Errorf("providers.codex.adapter.max_stdout_bytes must be between 1024 and 4194304")
	}
	return nil
}

func applyRawRelayBalance(relay *RelayBalanceConfig, raw *rawConfig) error {
	value := &raw.Providers.RelayBalance
	if value.Enabled != nil {
		relay.Enabled = *value.Enabled
	}
	assignNonEmpty(&relay.Endpoint, strings.TrimSpace(value.Endpoint))
	assignNonEmpty(&relay.SecretName, strings.TrimSpace(value.SecretName))
	for name, input := range map[string]string{
		"refresh_interval": value.RefreshInterval, "timeout": value.Timeout, "stale_after": value.StaleAfter,
	} {
		if input == "" {
			continue
		}
		parsed, err := positiveDuration("providers.relay_balance."+name, input)
		if err != nil {
			return err
		}
		switch name {
		case "refresh_interval":
			relay.RefreshInterval = parsed
		case "timeout":
			relay.Timeout = parsed
		case "stale_after":
			relay.StaleAfter = parsed
		}
	}
	if !relay.Enabled {
		return nil
	}
	if relay.Endpoint != "https://api.0-0.pro/v1/usage" {
		return fmt.Errorf("providers.relay_balance.endpoint must be https://api.0-0.pro/v1/usage")
	}
	if relay.SecretName == "" {
		return fmt.Errorf("providers.relay_balance.secret_name is required")
	}
	return nil
}

func positiveDuration(name, raw string) (time.Duration, error) {
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func applyRawWeather(weather *WeatherConfig, raw *rawConfig) {
	value := &raw.Providers.Weather
	if value.Enabled != nil {
		weather.Enabled = *value.Enabled
	}
	assignNonEmpty(&weather.Provider, value.Provider)
	assignNonEmpty(&weather.APIHost, strings.ToLower(strings.TrimSpace(value.APIHost)))
	assignNonEmpty(&weather.ProjectID, strings.TrimSpace(value.ProjectID))
	assignNonEmpty(&weather.CredentialID, strings.TrimSpace(value.CredentialID))
	assignNonEmpty(&weather.PrivateKeyPath, expandUserPath(value.PrivateKeyPath))
	assignNonEmpty(&weather.PublicKeyPath, expandUserPath(value.PublicKeyPath))
	assignNonEmpty(&weather.Location, strings.TrimSpace(value.Location))
	assignNonEmpty(&weather.LocationLabel, strings.TrimSpace(value.LocationLabel))
	assignNonEmpty(&weather.Timezone, strings.TrimSpace(value.Timezone))
	assignNonEmpty(&weather.Lang, strings.TrimSpace(value.Lang))
	assignNonEmpty(&weather.Schedule.Lunch, value.Schedule.Lunch)
	assignNonEmpty(&weather.Schedule.Leave, value.Schedule.Leave)
	if value.Schedule.ActiveWeekdays != nil {
		weather.Schedule.ActiveWeekdays = append([]int(nil), value.Schedule.ActiveWeekdays...)
	}
	weather.Refresh.Now = parseDurationOrKeep(value.Refresh.Now, weather.Refresh.Now)
	weather.Refresh.Hourly = parseDurationOrKeep(value.Refresh.Hourly, weather.Refresh.Hourly)
	weather.Refresh.RequestTimeout = parseDurationOrKeep(value.Refresh.RequestTimeout, weather.Refresh.RequestTimeout)
	if value.Refresh.ForceBeforeOuting != nil {
		weather.Refresh.ForceBeforeOuting = make([]time.Duration, len(value.Refresh.ForceBeforeOuting))
		for index, rawDuration := range value.Refresh.ForceBeforeOuting {
			weather.Refresh.ForceBeforeOuting[index] = parseDurationOrKeep(rawDuration, -1)
		}
	}
	weather.Cache.NowStaleAfter = parseDurationOrKeep(value.Cache.NowStaleAfter, weather.Cache.NowStaleAfter)
	weather.Cache.HourlyStaleAfter = parseDurationOrKeep(value.Cache.HourlyStaleAfter, weather.Cache.HourlyStaleAfter)
	if value.Cache.PersistLastGood != nil {
		weather.Cache.PersistLastGood = *value.Cache.PersistLastGood
	}
	weather.Umbrella.WindowBefore = parseDurationOrKeep(value.Umbrella.WindowBefore, weather.Umbrella.WindowBefore)
	weather.Umbrella.WindowAfter = parseDurationOrKeep(value.Umbrella.WindowAfter, weather.Umbrella.WindowAfter)
	if value.Umbrella.POPThreshold != nil {
		weather.Umbrella.POPThreshold = *value.Umbrella.POPThreshold
	}
	weather.Umbrella.RepeatBeforeOuting = parseDurationOrKeep(value.Umbrella.RepeatBeforeOuting, weather.Umbrella.RepeatBeforeOuting)
	if value.Satellite.Enabled != nil {
		weather.Satellite.Enabled = *value.Satellite.Enabled
	}
	if value.Satellite.Latitude != nil {
		weather.Satellite.Latitude = *value.Satellite.Latitude
	}
	if value.Satellite.Longitude != nil {
		weather.Satellite.Longitude = *value.Satellite.Longitude
	}
	assignNonEmpty(&weather.Satellite.LunchRefresh, value.Satellite.LunchRefresh)
	assignNonEmpty(&weather.Satellite.LeaveRefresh, value.Satellite.LeaveRefresh)
	weather.Satellite.StaleAfter = parseDurationOrKeep(value.Satellite.StaleAfter, weather.Satellite.StaleAfter)
	if value.Satellite.DirectRequired != nil {
		weather.Satellite.DirectRequired = *value.Satellite.DirectRequired
	}
	if value.Satellite.GHIRequired != nil {
		weather.Satellite.GHIRequired = *value.Satellite.GHIRequired
	}
	if value.Satellite.RequiredDirectShare != nil {
		weather.Satellite.RequiredDirectShare = *value.Satellite.RequiredDirectShare
	}
	if value.Satellite.DirectSuggested != nil {
		weather.Satellite.DirectSuggested = *value.Satellite.DirectSuggested
	}
	if value.Satellite.GHISuggested != nil {
		weather.Satellite.GHISuggested = *value.Satellite.GHISuggested
	}
	if value.Satellite.SuggestedDirectShare != nil {
		weather.Satellite.SuggestedDirectShare = *value.Satellite.SuggestedDirectShare
	}
}

func assignNonEmpty(target *string, value string) {
	if value != "" {
		*target = value
	}
}

func parseDurationOrKeep(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return -1
	}
	return value
}

func expandUserPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

var (
	qweatherHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.qweatherapi\.com$`)
	locationIDPattern   = regexp.MustCompile(`^[0-9]+$`)
	coordinatePattern   = regexp.MustCompile(`^-?[0-9]{1,3}(?:\.[0-9]{1,2})?,-?[0-9]{1,2}(?:\.[0-9]{1,2})?$`)
	clockPattern        = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
)

func ValidateWeather(weather WeatherConfig) error {
	if weather.Provider != "qweather" {
		return fmt.Errorf("providers.weather.provider must be qweather")
	}
	if !qweatherHostPattern.MatchString(weather.APIHost) {
		return fmt.Errorf("providers.weather.api_host must be an account-specific *.qweatherapi.com hostname without scheme or path")
	}
	if weather.ProjectID == "" {
		return fmt.Errorf("providers.weather.project_id is required")
	}
	if weather.CredentialID == "" {
		return fmt.Errorf("providers.weather.credential_id is required")
	}
	if weather.PrivateKeyPath == "" {
		return fmt.Errorf("providers.weather.private_key_path is required")
	}
	if err := validateLocation(weather.Location); err != nil {
		return fmt.Errorf("providers.weather.location: %w", err)
	}
	if weather.LocationLabel == "" {
		return fmt.Errorf("providers.weather.location_label is required")
	}
	if _, err := time.LoadLocation(weather.Timezone); err != nil {
		return fmt.Errorf("providers.weather.timezone is invalid: %w", err)
	}
	if weather.Lang == "" {
		return fmt.Errorf("providers.weather.lang is required")
	}
	if !clockPattern.MatchString(weather.Schedule.Lunch) || !clockPattern.MatchString(weather.Schedule.Leave) {
		return fmt.Errorf("providers.weather.schedule lunch and leave must use HH:MM")
	}
	seenWeekdays := make(map[int]bool)
	if len(weather.Schedule.ActiveWeekdays) == 0 {
		return fmt.Errorf("providers.weather.schedule.active_weekdays must not be empty")
	}
	for _, weekday := range weather.Schedule.ActiveWeekdays {
		if weekday < 1 || weekday > 7 || seenWeekdays[weekday] {
			return fmt.Errorf("providers.weather.schedule.active_weekdays must contain unique ISO weekdays 1 through 7")
		}
		seenWeekdays[weekday] = true
	}
	for name, duration := range map[string]time.Duration{
		"refresh.now": weather.Refresh.Now, "refresh.hourly": weather.Refresh.Hourly,
		"refresh.request_timeout": weather.Refresh.RequestTimeout, "cache.now_stale_after": weather.Cache.NowStaleAfter,
		"cache.hourly_stale_after": weather.Cache.HourlyStaleAfter, "umbrella.window_before": weather.Umbrella.WindowBefore,
		"umbrella.window_after": weather.Umbrella.WindowAfter, "umbrella.repeat_before_outing": weather.Umbrella.RepeatBeforeOuting,
	} {
		if duration <= 0 {
			return fmt.Errorf("providers.weather.%s must be a positive duration", name)
		}
	}
	for _, duration := range weather.Refresh.ForceBeforeOuting {
		if duration <= 0 {
			return fmt.Errorf("providers.weather.refresh.force_before_outing must contain positive durations")
		}
	}
	if weather.Umbrella.POPThreshold < 0 || weather.Umbrella.POPThreshold > 100 {
		return fmt.Errorf("providers.weather.umbrella.pop_threshold must be between 0 and 100")
	}
	if err := validateSatelliteRadiation(weather.Satellite); err != nil {
		return err
	}
	return nil
}

func validateSatelliteRadiation(satellite SatelliteRadiationConfig) error {
	if !satellite.Enabled {
		return nil
	}
	if satellite.Latitude < -90 || satellite.Latitude > 90 || satellite.Longitude < -180 || satellite.Longitude > 180 {
		return fmt.Errorf("providers.weather.satellite_radiation latitude/longitude are outside valid ranges")
	}
	if !clockPattern.MatchString(satellite.LunchRefresh) || !clockPattern.MatchString(satellite.LeaveRefresh) {
		return fmt.Errorf("providers.weather.satellite_radiation lunch_refresh and leave_refresh must use HH:MM")
	}
	if satellite.StaleAfter <= 0 {
		return fmt.Errorf("providers.weather.satellite_radiation.stale_after must be a positive duration")
	}
	if satellite.DirectSuggested < 0 || satellite.DirectRequired < satellite.DirectSuggested ||
		satellite.GHISuggested < 0 || satellite.GHIRequired < satellite.GHISuggested {
		return fmt.Errorf("providers.weather.satellite_radiation required thresholds must be at least their suggested thresholds")
	}
	if satellite.SuggestedDirectShare < 0 || satellite.SuggestedDirectShare > 1 ||
		satellite.RequiredDirectShare < satellite.SuggestedDirectShare || satellite.RequiredDirectShare > 1 {
		return fmt.Errorf("providers.weather.satellite_radiation direct-share thresholds must be ordered values between 0 and 1")
	}
	return nil
}

func validateLocation(value string) error {
	if locationIDPattern.MatchString(value) {
		return nil
	}
	if !coordinatePattern.MatchString(value) {
		return fmt.Errorf("must be a LocationID or longitude,latitude with at most two decimal places")
	}
	parts := strings.Split(value, ",")
	longitude, longitudeErr := strconv.ParseFloat(parts[0], 64)
	latitude, latitudeErr := strconv.ParseFloat(parts[1], 64)
	if longitudeErr != nil || latitudeErr != nil || net.ParseIP(value) != nil || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90 {
		return fmt.Errorf("coordinates are outside valid longitude/latitude ranges")
	}
	return nil
}
