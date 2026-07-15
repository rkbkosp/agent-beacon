package qweather

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
)

type memoryCache struct {
	mu      sync.Mutex
	records []CacheRecord
	saves   int
}

func (cache *memoryCache) Load() ([]CacheRecord, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return append([]CacheRecord(nil), cache.records...), nil
}

func (cache *memoryCache) Save(records []CacheRecord) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.records = append([]CacheRecord(nil), records...)
	cache.saves++
	return nil
}

func (cache *memoryCache) Clear() error {
	cache.mu.Lock()
	cache.records = nil
	cache.mu.Unlock()
	return nil
}

type fakeWeatherClient struct {
	mu          sync.Mutex
	nowData     NowData
	hourlyData  HourlyData
	nowErr      error
	hourlyErr   error
	nowCalls    int
	hourlyCalls int
	block       <-chan struct{}
}

type fakeRadiationClient struct {
	data  RadiationData
	err   error
	calls int
}

func (client *fakeRadiationClient) FetchRadiation(context.Context) (RadiationData, error) {
	client.calls++
	return client.data, client.err
}

func (client *fakeWeatherClient) FetchNow(ctx context.Context) (NowData, error) {
	client.mu.Lock()
	client.nowCalls++
	block := client.block
	data, err := client.nowData, client.nowErr
	client.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return NowData{}, ctx.Err()
		}
	}
	return data, err
}

func (client *fakeWeatherClient) FetchHourly(ctx context.Context, horizon time.Duration) (HourlyData, error) {
	client.mu.Lock()
	client.hourlyCalls++
	block := client.block
	data, err := client.hourlyData, client.hourlyErr
	client.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return HourlyData{}, ctx.Err()
		}
	}
	return data, err
}

func providerNow(at time.Time, temperature int) NowData {
	precip := 0.0
	return NowData{UpdateTime: at, ObservedAt: at, Temp: &temperature, Icon: "101", Text: "多云", Precip: &precip,
		Refer: Refer{Sources: []string{"QWeather"}, License: []string{"license"}}, FetchedAt: at,
		Raw: json.RawMessage(`{"code":"200","updateTime":"2026-07-14T10:00+08:00","now":{"obsTime":"2026-07-14T10:00+08:00","temp":"29","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`)}
}

func providerHourly(at, target time.Time, wet bool) HourlyData {
	temperature, pop := 27, 10
	precip, icon, text := 0.0, "100", "晴"
	if wet {
		pop, precip, icon, text = 70, 0.5, "305", "小雨"
	}
	points := []HourlyPoint{
		{ForecastAt: target.Add(-time.Hour), Temp: &temperature, POP: &pop, Precip: &precip, Icon: icon, Text: text},
		{ForecastAt: target, Temp: &temperature, POP: &pop, Precip: &precip, Icon: icon, Text: text},
		{ForecastAt: target.Add(time.Hour), Temp: &temperature, POP: &pop, Precip: &precip, Icon: icon, Text: text},
	}
	return HourlyData{Endpoint: "/v7/weather/24h", UpdateTime: at, Points: points,
		Refer: Refer{Sources: []string{"QWeather"}, License: []string{"license"}}, FetchedAt: at,
		Raw: json.RawMessage(`{"code":"200","updateTime":"2026-07-14T10:00+08:00","hourly":[{"fxTime":"2026-07-14T12:00+08:00","temp":"27","icon":"305","text":"小雨","pop":"70","precip":"0.5"}],"refer":{"sources":["QWeather"],"license":["license"]}}`)}
}

func TestNextCSTForcedRefresh(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before lunch refresh",
			now:  shanghaiTime(t, 2026, time.July, 14, 11, 54),
			want: shanghaiTime(t, 2026, time.July, 14, 11, 55),
		},
		{
			name: "after lunch refresh",
			now:  shanghaiTime(t, 2026, time.July, 14, 11, 55),
			want: shanghaiTime(t, 2026, time.July, 14, 18, 20),
		},
		{
			name: "before evening refresh from UTC",
			now:  time.Date(2026, time.July, 14, 10, 19, 30, 0, time.UTC),
			want: time.Date(2026, time.July, 14, 10, 20, 0, 0, time.UTC),
		},
		{
			name: "after evening refresh",
			now:  shanghaiTime(t, 2026, time.July, 14, 18, 20),
			want: shanghaiTime(t, 2026, time.July, 15, 11, 55),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nextCSTForcedRefresh(test.now); !got.Equal(test.want) {
				t.Fatalf("next forced refresh = %s, want %s", got, test.want)
			}
		})
	}
}

func TestNextRadiationRefreshUsesExactCSTCheckpoints(t *testing.T) {
	satellite := config.Default().Providers.Weather.Satellite
	tests := []struct {
		name     string
		now      time.Time
		want     time.Time
		wantSlot string
	}{
		{name: "before lunch", now: shanghaiTime(t, 2026, time.July, 15, 11, 56), want: shanghaiTime(t, 2026, time.July, 15, 11, 57), wantSlot: "lunch"},
		{name: "after lunch", now: shanghaiTime(t, 2026, time.July, 15, 11, 57), want: shanghaiTime(t, 2026, time.July, 15, 18, 28), wantSlot: "leave"},
		{name: "before leave from UTC", now: time.Date(2026, time.July, 15, 10, 27, 30, 0, time.UTC), want: time.Date(2026, time.July, 15, 10, 28, 0, 0, time.UTC), wantSlot: "leave"},
		{name: "after leave", now: shanghaiTime(t, 2026, time.July, 15, 18, 28), want: shanghaiTime(t, 2026, time.July, 16, 11, 57), wantSlot: "lunch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextRadiationRefresh(test.now, satellite)
			if err != nil {
				t.Fatal(err)
			}
			if !got.at.Equal(test.want) || got.slot != test.wantSlot {
				t.Fatalf("next radiation refresh = %s %s, want %s %s", got.at, got.slot, test.want, test.wantSlot)
			}
		})
	}
}

func TestRadiationRefreshAddsSunshadeDecisionNotificationAndPersistsCache(t *testing.T) {
	weather := testWeatherConfig()
	weather.Satellite.Enabled = true
	now := shanghaiTime(t, 2026, time.July, 15, 11, 57)
	weatherClient := &fakeWeatherClient{nowData: providerNow(now, 32), hourlyData: providerHourly(now, shanghaiTime(t, 2026, time.July, 15, 12, 0), false)}
	radiationClient := &fakeRadiationClient{data: RadiationData{
		ObservedAt: shanghaiTime(t, 2026, time.July, 15, 11, 20), GHI: 701, Direct: 502,
		Diffuse: 190, DNI: 550, Terrestrial: 1070, DirectShare: 502.0 / 701.0,
		FetchedAt: now, Raw: json.RawMessage(satelliteFixture),
	}}
	cache := &memoryCache{}
	provider, err := New(weather, weatherClient, cache, WithRadiationClient(radiationClient),
		WithClock(func() time.Time { return now }), WithJitter(func(value time.Duration) time.Duration { return value }))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := provider.Refresh(context.Background(), true)
	if err != nil || initial[0].Patch.Weather == nil || initial[0].Patch.Weather.NextOuting.Reason != "无雨" {
		t.Fatalf("initial refresh = %+v, err=%v", initial, err)
	}
	updates, err := provider.RefreshRadiation(context.Background(), "lunch")
	if err != nil {
		t.Fatal(err)
	}
	if radiationClient.calls != 1 || len(updates) < 2 || updates[0].Patch.Weather == nil {
		t.Fatalf("radiation updates = %+v, calls=%d", updates, radiationClient.calls)
	}
	outing := updates[0].Patch.Weather.NextOuting
	if outing.UmbrellaRequired == nil || !*outing.UmbrellaRequired || outing.Reason != "遮阳" || outing.Confidence != "high" {
		t.Fatalf("sunshade outing = %+v", outing)
	}
	found := false
	for _, update := range updates {
		if update.Notification != nil && update.Notification.Kind == "weather.umbrella_required" {
			found = update.Notification.Source == "open-meteo" && update.Notification.SourceLabel == "Open-Meteo" && update.Notification.Detail == "遮阳"
		}
	}
	if !found {
		t.Fatalf("open-meteo umbrella notification missing: %+v", updates)
	}
	if len(cache.records) == 0 || cache.records[len(cache.records)-1].Provider != "open-meteo" || cache.records[len(cache.records)-1].Slot != "lunch" {
		t.Fatalf("radiation cache records = %+v", cache.records)
	}

	restored, err := New(weather, &fakeWeatherClient{}, cache, WithRadiationClient(&fakeRadiationClient{}),
		WithClock(func() time.Time { return now.Add(time.Minute) }))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := restored.Snapshot(context.Background())
	if err != nil || patch.Weather == nil || patch.Weather.NextOuting.Reason != "遮阳" {
		t.Fatalf("restored radiation snapshot = %+v, err=%v", patch.Weather, err)
	}
}

func TestProviderStartsFromCacheThenRefreshesBothEndpoints(t *testing.T) {
	config := testWeatherConfig()
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	cachedNow := providerNow(now.Add(-5*time.Minute), 25)
	cachedHourly := providerHourly(now.Add(-5*time.Minute), shanghaiTime(t, 2026, time.July, 14, 12, 0), false)
	cache := &memoryCache{records: []CacheRecord{
		{Provider: "qweather", Endpoint: "/v7/weather/now", Location: config.Location, FetchedAt: cachedNow.FetchedAt, UpdateTime: cachedNow.UpdateTime.Format(time.RFC3339), PayloadJSON: cachedNow.Raw},
		{Provider: "qweather", Endpoint: "/v7/weather/24h", Location: config.Location, FetchedAt: cachedHourly.FetchedAt, UpdateTime: cachedHourly.UpdateTime.Format(time.RFC3339), PayloadJSON: cachedHourly.Raw},
	}}
	client := &fakeWeatherClient{nowData: providerNow(now, 29), hourlyData: providerHourly(now, shanghaiTime(t, 2026, time.July, 14, 12, 0), false)}
	provider, err := New(config, client, cache, WithClock(func() time.Time { return now }), WithJitter(func(value time.Duration) time.Duration { return value }))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan providers.Update, 8)
	done := make(chan error, 1)
	go func() { done <- provider.Start(ctx, updates) }()
	first := <-updates
	second := <-updates
	cancel()
	<-done
	if first.Patch.Weather == nil || first.Patch.Weather.Current.Freshness != protocol.FreshnessCached || first.Patch.Weather.Current.TempC != 29 {
		t.Fatalf("cached startup state = %+v", first.Patch.Weather)
	}
	if second.Patch.Weather == nil || second.Patch.Weather.Current.Freshness != protocol.FreshnessFresh || second.Patch.Weather.Current.TempC != 29 {
		t.Fatalf("live state = %+v", second.Patch.Weather)
	}
	if client.nowCalls != 1 || client.hourlyCalls != 1 || cache.saves == 0 {
		t.Fatalf("calls now=%d hourly=%d saves=%d", client.nowCalls, client.hourlyCalls, cache.saves)
	}
}

func TestProviderRetainsLastGoodAndMarksStaleAfterFailures(t *testing.T) {
	config := testWeatherConfig()
	currentTime := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	target := shanghaiTime(t, 2026, time.July, 14, 12, 0)
	client := &fakeWeatherClient{nowData: providerNow(currentTime, 29), hourlyData: providerHourly(currentTime, target, false)}
	provider, err := New(config, client, &memoryCache{}, WithClock(func() time.Time { return currentTime }), WithJitter(func(value time.Duration) time.Duration { return value }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	currentTime = currentTime.Add(2 * time.Hour)
	client.nowErr = errors.New("network down")
	client.hourlyErr = errors.New("network down")
	updates, err := provider.Refresh(context.Background(), true)
	if err == nil {
		t.Fatal("failed refresh must report an error")
	}
	if len(updates) < 2 || updates[0].Patch.Weather == nil {
		t.Fatalf("updates = %+v", updates)
	}
	weather := updates[0].Patch.Weather
	if weather.Current.TempC != 29 || weather.Current.Freshness != protocol.FreshnessStale || weather.NextOuting.UmbrellaRequired != nil {
		t.Fatalf("last-good stale state = %+v", weather)
	}
	foundStale := false
	for _, update := range updates {
		if update.Notification != nil && update.Notification.Kind == "weather.data_stale" && update.Notification.Theme == protocol.ThemeYellow {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatalf("stale notification missing: %+v", updates)
	}
}

func TestProviderNotifiesOnRequiredTransitionAndOnceAtThirtyMinutes(t *testing.T) {
	config := testWeatherConfig()
	currentTime := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	target := shanghaiTime(t, 2026, time.July, 14, 12, 0)
	client := &fakeWeatherClient{nowData: providerNow(currentTime, 29), hourlyData: providerHourly(currentTime, target, true)}
	provider, err := New(config, client, &memoryCache{}, WithClock(func() time.Time { return currentTime }), WithJitter(func(value time.Duration) time.Duration { return value }))
	if err != nil {
		t.Fatal(err)
	}
	first, err := provider.Refresh(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if countNotification(first, "weather.umbrella_required", "weather:umbrella:2026-07-14:lunch") != 1 {
		t.Fatalf("first transition updates = %+v", first)
	}
	second, err := provider.Refresh(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if countNotification(second, "weather.umbrella_required", "") != 0 {
		t.Fatal("unchanged required state emitted a duplicate transition")
	}
	currentTime = shanghaiTime(t, 2026, time.July, 14, 11, 30)
	client.nowData = providerNow(currentTime, 29)
	client.hourlyData = providerHourly(currentTime, target, true)
	reminder, err := provider.Refresh(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if countNotification(reminder, "weather.umbrella_reminder", "weather:umbrella-reminder:2026-07-14:lunch") != 1 {
		t.Fatalf("reminder updates = %+v", reminder)
	}
	repeated, err := provider.Refresh(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if countNotification(repeated, "weather.umbrella_reminder", "") != 0 {
		t.Fatal("30-minute reminder was emitted more than once")
	}
}

func countNotification(updates []providers.Update, kind, dedupe string) int {
	count := 0
	for _, update := range updates {
		if update.Notification != nil && update.Notification.Kind == kind && (dedupe == "" || update.Notification.DedupeKey == dedupe) {
			count++
		}
	}
	return count
}

func TestProviderCoalescesConcurrentRefreshes(t *testing.T) {
	config := testWeatherConfig()
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	release := make(chan struct{})
	client := &fakeWeatherClient{nowData: providerNow(now, 29), hourlyData: providerHourly(now, shanghaiTime(t, 2026, time.July, 14, 12, 0), false), block: release}
	provider, err := New(config, client, &memoryCache{}, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errorsOut := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, refreshErr := provider.Refresh(context.Background(), true)
			errorsOut <- refreshErr
		}()
	}
	time.Sleep(10 * time.Millisecond)
	close(release)
	group.Wait()
	close(errorsOut)
	for refreshErr := range errorsOut {
		if refreshErr != nil {
			t.Fatal(refreshErr)
		}
	}
	if client.nowCalls != 1 || client.hourlyCalls != 1 {
		t.Fatalf("coalesced calls now=%d hourly=%d", client.nowCalls, client.hourlyCalls)
	}
}

func TestProviderBackoffUsesRetryAfterAndBoundedExponentialSequence(t *testing.T) {
	config := testWeatherConfig()
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	provider, err := New(config, &fakeWeatherClient{}, &memoryCache{}, WithClock(func() time.Time { return now }), WithJitter(func(value time.Duration) time.Duration { return value }))
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.recordFailure(&APIError{StatusCode: 429, RetryAfter: 7 * time.Minute}); got != 7*time.Minute {
		t.Fatalf("Retry-After delay = %s", got)
	}
	provider.resetFailures()
	for index, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		if got := provider.recordFailure(&APIError{StatusCode: 503}); got != want {
			t.Fatalf("failure %d delay = %s, want %s", index+1, got, want)
		}
	}
}

func TestProviderDoesNotJitterExplicitRetryAfter(t *testing.T) {
	config := testWeatherConfig()
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	provider, err := New(config, &fakeWeatherClient{}, &memoryCache{}, WithClock(func() time.Time { return now }),
		WithJitter(func(value time.Duration) time.Duration { return value * 2 }))
	if err != nil {
		t.Fatal(err)
	}
	if got := provider.recordFailure(&APIError{StatusCode: 429, RetryAfter: 7 * time.Minute}); got != 7*time.Minute {
		t.Fatalf("explicit Retry-After was modified to %s", got)
	}
	provider.resetFailures()
	if got := provider.recordFailure(&APIError{StatusCode: 503}); got != 2*time.Minute {
		t.Fatalf("ordinary exponential delay did not use jitter: %s", got)
	}
}
