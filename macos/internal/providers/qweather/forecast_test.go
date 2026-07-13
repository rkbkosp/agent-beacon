package qweather

import (
	"testing"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
)

func testWeatherConfig() config.WeatherConfig {
	weather := config.Default().Providers.Weather
	weather.Enabled = true
	weather.APIHost = "abc.qweatherapi.com"
	weather.ProjectID = "project"
	weather.CredentialID = "credential"
	weather.PrivateKeyPath = "/tmp/private.pem"
	weather.Location = "101210101"
	weather.LocationLabel = "杭州"
	return weather
}

func shanghaiTime(t *testing.T, year int, month time.Month, day, hour, minute int) time.Time {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(year, month, day, hour, minute, 0, 0, location)
}

func TestTargetsCoverDailyAndWeekendBoundaries(t *testing.T) {
	config := testWeatherConfig()
	for _, testCase := range []struct {
		name       string
		now        time.Time
		wantLunch  string
		wantLeave  string
		wantSlot   string
		wantOuting string
	}{{"before lunch", shanghaiTime(t, 2026, time.July, 13, 11, 59), "2026-07-13T12:00:00+08:00", "2026-07-13T19:00:00+08:00", "lunch", "2026-07-13T12:00:00+08:00"},
		{"at lunch", shanghaiTime(t, 2026, time.July, 13, 12, 0), "2026-07-13T12:00:00+08:00", "2026-07-13T19:00:00+08:00", "leave", "2026-07-13T19:00:00+08:00"},
		{"before leave", shanghaiTime(t, 2026, time.July, 13, 18, 59), "2026-07-13T12:00:00+08:00", "2026-07-13T19:00:00+08:00", "leave", "2026-07-13T19:00:00+08:00"},
		{"at leave", shanghaiTime(t, 2026, time.July, 13, 19, 0), "2026-07-14T12:00:00+08:00", "2026-07-14T19:00:00+08:00", "lunch", "2026-07-14T12:00:00+08:00"},
		{"friday after leave", shanghaiTime(t, 2026, time.July, 17, 19, 0), "2026-07-20T12:00:00+08:00", "2026-07-20T19:00:00+08:00", "lunch", "2026-07-20T12:00:00+08:00"},
		{"saturday", shanghaiTime(t, 2026, time.July, 18, 8, 0), "2026-07-20T12:00:00+08:00", "2026-07-20T19:00:00+08:00", "lunch", "2026-07-20T12:00:00+08:00"}} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := TargetsFor(testCase.now.UTC(), config.Timezone, config.Schedule)
			if err != nil {
				t.Fatal(err)
			}
			if got.Lunch.Format(time.RFC3339) != testCase.wantLunch || got.Leave.Format(time.RFC3339) != testCase.wantLeave ||
				got.NextSlot != testCase.wantSlot || got.NextOuting.Format(time.RFC3339) != testCase.wantOuting {
				t.Fatalf("targets = %+v", got)
			}
		})
	}
}

func TestSelectForecastSortsAndUsesExactThenNearestWithoutCrossingDate(t *testing.T) {
	target := shanghaiTime(t, 2026, time.July, 14, 12, 0)
	points := []HourlyPoint{
		{ForecastAt: shanghaiTime(t, 2026, time.July, 15, 12, 0), Text: "wrong date"},
		{ForecastAt: target.Add(60 * time.Minute), Text: "too far"},
		{ForecastAt: target.Add(-59 * time.Minute), Text: "nearest"},
		{ForecastAt: target, Text: "exact"},
	}
	got, ok := SelectForecast(points, target)
	if !ok || got.Text != "exact" {
		t.Fatalf("exact selection = %+v, %v", got, ok)
	}
	points = points[:3]
	got, ok = SelectForecast(points, target)
	if !ok || got.Text != "nearest" {
		t.Fatalf("nearest selection = %+v, %v", got, ok)
	}
	if _, ok := SelectForecast(points[:2], target); ok {
		t.Fatal("60-minute or cross-date forecast was accepted")
	}
}

func TestRequiredHorizonUses72HoursOnlyBeyond24Hours(t *testing.T) {
	now := shanghaiTime(t, 2026, time.July, 14, 12, 0)
	targets := Targets{Lunch: now.Add(2 * time.Hour), Leave: now.Add(24 * time.Hour), NextOuting: now.Add(time.Hour)}
	if got := RequiredHorizon(now, targets); got != 24*time.Hour {
		t.Fatalf("24-hour horizon = %s", got)
	}
	targets.Leave = now.Add(24*time.Hour + time.Second)
	if got := RequiredHorizon(now, targets); got <= 24*time.Hour {
		t.Fatalf("72-hour horizon trigger = %s", got)
	}
}

func TestBuildWeatherStateMapsTargetsFreshnessAndUnknownDecision(t *testing.T) {
	config := testWeatherConfig()
	now := shanghaiTime(t, 2026, time.July, 14, 14, 30)
	temperature := 29
	precipitation := 0.0
	pop := 10
	current := &NowData{ObservedAt: now.Add(-20 * time.Minute), Temp: &temperature, Icon: "101", Text: "多云", Precip: &precipitation,
		FetchedAt: now.Add(-5 * time.Minute), UpdateTime: now.Add(-10 * time.Minute), Refer: Refer{Sources: []string{"QWeather"}, License: []string{"license"}}}
	hourly := &HourlyData{FetchedAt: now.Add(-2 * time.Hour), UpdateTime: now.Add(-2 * time.Hour), Points: []HourlyPoint{
		{ForecastAt: shanghaiTime(t, 2026, time.July, 14, 12, 0), Temp: &temperature, Icon: "100", Text: "晴", POP: &pop, Precip: &precipitation},
		{ForecastAt: shanghaiTime(t, 2026, time.July, 14, 19, 0), Temp: &temperature, Icon: "100", Text: "晴", POP: &pop, Precip: &precipitation},
	}}
	got, err := BuildWeatherState(now, config, current, hourly)
	if err != nil {
		t.Fatal(err)
	}
	if got.Location != "杭州" || got.Provider != "qweather" || got.Current.TempC != 29 || got.Current.Freshness != protocol.FreshnessFresh {
		t.Fatalf("current state = %+v", got)
	}
	if got.Lunch.TargetAt.Hour() != 12 || got.Leave.TargetAt.Hour() != 19 || !got.Lunch.IsPast || got.Leave.IsPast {
		t.Fatalf("weather slots = lunch:%+v leave:%+v", got.Lunch, got.Leave)
	}
	if got.Leave.Freshness != protocol.FreshnessStale || got.NextOuting.UmbrellaRequired != nil || got.NextOuting.Confidence != "unknown" {
		t.Fatalf("stale outing = %+v", got.NextOuting)
	}
}
