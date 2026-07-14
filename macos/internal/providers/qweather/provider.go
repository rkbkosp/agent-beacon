package qweather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
)

type WeatherClient interface {
	FetchNow(context.Context) (NowData, error)
	FetchHourly(context.Context, time.Duration) (HourlyData, error)
}

type Option func(*Provider)

func WithClock(now func() time.Time) Option {
	return func(provider *Provider) { provider.now = now }
}

func WithJitter(jitter func(time.Duration) time.Duration) Option {
	return func(provider *Provider) { provider.jitter = jitter }
}

type refreshMask uint8

const (
	refreshNow refreshMask = 1 << iota
	refreshHourly
	refreshAll = refreshNow | refreshHourly
)

type dailyRefreshTime struct {
	hour   int
	minute int
}

var (
	chinaStandardTime  = time.FixedZone("CST", 8*60*60)
	forcedRefreshTimes = [...]dailyRefreshTime{
		{hour: 11, minute: 55},
		{hour: 18, minute: 20},
	}
)

type providerFlight struct {
	done    chan struct{}
	updates []providers.Update
	err     error
}

type Provider struct {
	config config.WeatherConfig
	client WeatherClient
	cache  CacheStore
	now    func() time.Time
	jitter func(time.Duration) time.Duration

	mu                     sync.Mutex
	current                *NowData
	hourly                 *HourlyData
	cacheRecords           []CacheRecord
	cacheLoaded            bool
	failures               int
	nextRetryAt            time.Time
	lastError              string
	lastWindowKey          string
	lastDecision           string
	reminded               map[string]bool
	staleNotified          bool
	forcedRefreshes        map[string]bool
	lastScheduledWindowKey string

	flightsMu sync.Mutex
	flights   map[refreshMask]*providerFlight
}

func New(weather config.WeatherConfig, client WeatherClient, cache CacheStore, options ...Option) (*Provider, error) {
	if !weather.Enabled {
		return nil, errors.New("qweather provider is disabled")
	}
	if err := config.ValidateWeather(weather); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("qweather client is required")
	}
	if cache == nil {
		return nil, errors.New("qweather cache is required")
	}
	provider := &Provider{
		config: weather, client: client, cache: cache, now: time.Now, jitter: defaultJitter,
		reminded: make(map[string]bool), forcedRefreshes: make(map[string]bool), flights: make(map[refreshMask]*providerFlight),
	}
	for _, option := range options {
		option(provider)
	}
	if provider.now == nil || provider.jitter == nil {
		return nil, errors.New("qweather clock and jitter functions are required")
	}
	return provider, nil
}

func (provider *Provider) Name() string { return "qweather" }

func (provider *Provider) Start(ctx context.Context, output chan<- providers.Update) error {
	if err := provider.loadCache(); err != nil {
		provider.mu.Lock()
		provider.lastError = err.Error()
		provider.mu.Unlock()
	}
	if update, ok := provider.cachedUpdate(); ok {
		if err := sendProviderUpdate(ctx, output, update); err != nil {
			return err
		}
	}
	updates, _ := provider.Refresh(ctx, true)
	if err := sendProviderUpdates(ctx, output, updates); err != nil {
		return err
	}

	nowTicker := time.NewTicker(provider.config.Refresh.Now)
	hourlyTicker := time.NewTicker(provider.config.Refresh.Hourly)
	minuteTicker := time.NewTicker(time.Minute)
	forcedRefreshTimer := newForcedRefreshTimer(provider.now())
	defer nowTicker.Stop()
	defer hourlyTicker.Stop()
	defer minuteTicker.Stop()
	defer forcedRefreshTimer.Stop()
	for {
		var next []providers.Update
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-nowTicker.C:
			next, _ = provider.refresh(ctx, refreshNow, false)
		case <-hourlyTicker.C:
			next, _ = provider.refresh(ctx, refreshHourly, false)
		case <-minuteTicker.C:
			next, _ = provider.minuteUpdates(ctx)
		case <-forcedRefreshTimer.C:
			next, _ = provider.Refresh(ctx, true)
			resetForcedRefreshTimer(forcedRefreshTimer, provider.now())
		}
		if err := sendProviderUpdates(ctx, output, next); err != nil {
			return err
		}
	}
}

func nextCSTForcedRefresh(now time.Time) time.Time {
	localNow := now.In(chinaStandardTime)
	for _, scheduled := range forcedRefreshTimes {
		candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(),
			scheduled.hour, scheduled.minute, 0, 0, chinaStandardTime)
		if candidate.After(localNow) {
			return candidate
		}
	}
	tomorrow := localNow.AddDate(0, 0, 1)
	first := forcedRefreshTimes[0]
	return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(),
		first.hour, first.minute, 0, 0, chinaStandardTime)
}

func newForcedRefreshTimer(now time.Time) *time.Timer {
	return time.NewTimer(nextCSTForcedRefresh(now).Sub(now))
}

func resetForcedRefreshTimer(timer *time.Timer, now time.Time) {
	timer.Reset(nextCSTForcedRefresh(now).Sub(now))
}

func (provider *Provider) Snapshot(context.Context) (protocol.StatePatch, error) {
	if err := provider.loadCache(); err != nil {
		return protocol.StatePatch{}, err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	state, err := BuildWeatherState(provider.now(), provider.config, provider.current, provider.hourly)
	if err != nil {
		return protocol.StatePatch{}, err
	}
	return protocol.StatePatch{Weather: &state}, nil
}

func (provider *Provider) Health(context.Context) providers.Health {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.failures > 0 || provider.lastError != "" {
		detail := provider.lastError
		if !provider.nextRetryAt.IsZero() {
			detail = fmt.Sprintf("%s; next retry %s", detail, provider.nextRetryAt.Format(time.RFC3339))
		}
		return providers.Health{Healthy: false, Detail: detail}
	}
	if provider.current == nil || provider.hourly == nil {
		return providers.Health{Healthy: false, Detail: "qweather has no successful now/hourly data"}
	}
	return providers.Health{Healthy: true, Detail: "qweather now/hourly data available"}
}

func (provider *Provider) Refresh(ctx context.Context, force bool) ([]providers.Update, error) {
	return provider.refresh(ctx, refreshAll, force)
}

func (provider *Provider) refresh(ctx context.Context, mask refreshMask, force bool) ([]providers.Update, error) {
	provider.flightsMu.Lock()
	if existing := provider.flights[mask]; existing != nil {
		provider.flightsMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-existing.done:
			return append([]providers.Update(nil), existing.updates...), existing.err
		}
	}
	flight := &providerFlight{done: make(chan struct{})}
	provider.flights[mask] = flight
	provider.flightsMu.Unlock()

	flight.updates, flight.err = provider.performRefresh(ctx, mask, force)
	provider.flightsMu.Lock()
	delete(provider.flights, mask)
	close(flight.done)
	provider.flightsMu.Unlock()
	return append([]providers.Update(nil), flight.updates...), flight.err
}

func (provider *Provider) performRefresh(ctx context.Context, mask refreshMask, force bool) ([]providers.Update, error) {
	if err := provider.loadCache(); err != nil {
		provider.mu.Lock()
		provider.lastError = err.Error()
		provider.mu.Unlock()
	}
	now := provider.now()
	provider.mu.Lock()
	retryAt := provider.nextRetryAt
	provider.mu.Unlock()
	if !force && retryAt.After(now) {
		return nil, nil
	}

	var current NowData
	var hourly HourlyData
	var nowErr, hourlyErr error
	if mask&refreshNow != 0 {
		current, nowErr = provider.client.FetchNow(ctx)
	}
	if mask&refreshHourly != 0 {
		targets, targetErr := TargetsFor(now, provider.config.Timezone, provider.config.Schedule)
		if targetErr != nil {
			hourlyErr = targetErr
		} else {
			hourly, hourlyErr = provider.client.FetchHourly(ctx, RequiredHorizon(now, targets))
		}
	}

	provider.mu.Lock()
	if nowErr == nil && mask&refreshNow != 0 {
		copy := current
		provider.current = &copy
		provider.replaceNowCacheRecordLocked(current)
	}
	if hourlyErr == nil && mask&refreshHourly != 0 {
		provider.hourly = mergeHourly(provider.hourly, hourly, now)
		provider.appendHourlyCacheRecordLocked(hourly)
	}
	records := append([]CacheRecord(nil), provider.cacheRecords...)
	updates, stateErr := provider.stateUpdatesLocked(now, hourlyErr == nil && mask&refreshHourly != 0)
	provider.mu.Unlock()

	var combined error
	if nowErr != nil {
		combined = errors.Join(combined, fmt.Errorf("refresh qweather now: %w", nowErr))
	}
	if hourlyErr != nil {
		combined = errors.Join(combined, fmt.Errorf("refresh qweather hourly: %w", hourlyErr))
	}
	if stateErr != nil {
		combined = errors.Join(combined, stateErr)
	}
	if (nowErr == nil && mask&refreshNow != 0 || hourlyErr == nil && mask&refreshHourly != 0) && provider.config.Cache.PersistLastGood {
		if err := provider.cache.Save(records); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	if combined != nil {
		provider.recordFailure(combined)
	} else {
		provider.resetFailures()
	}
	return updates, combined
}

func (provider *Provider) loadCache() error {
	provider.mu.Lock()
	if provider.cacheLoaded {
		provider.mu.Unlock()
		return nil
	}
	provider.cacheLoaded = true
	provider.mu.Unlock()
	if !provider.config.Cache.PersistLastGood {
		return nil
	}
	records, err := provider.cache.Load()
	if err != nil {
		return err
	}
	sort.SliceStable(records, func(left, right int) bool { return records[left].FetchedAt.Before(records[right].FetchedAt) })
	provider.mu.Lock()
	defer provider.mu.Unlock()
	for _, record := range records {
		if record.Provider != "qweather" || record.Location != provider.config.Location {
			continue
		}
		switch record.Endpoint {
		case "/v7/weather/now":
			parsed, parseErr := parseCachedNow(record)
			if parseErr != nil {
				continue
			}
			if provider.current == nil || parsed.FetchedAt.After(provider.current.FetchedAt) {
				provider.current = &parsed
			}
		case "/v7/weather/24h", "/v7/weather/72h":
			parsed, parseErr := parseCachedHourly(record)
			if parseErr != nil {
				continue
			}
			provider.hourly = mergeHourly(provider.hourly, parsed, provider.now())
		}
		provider.cacheRecords = append(provider.cacheRecords, record)
	}
	provider.pruneCacheRecordsLocked()
	return nil
}

func (provider *Provider) cachedUpdate() (providers.Update, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.current == nil && provider.hourly == nil {
		return providers.Update{}, false
	}
	state, err := BuildWeatherState(provider.now(), provider.config, provider.current, provider.hourly)
	if err != nil {
		return providers.Update{}, false
	}
	provider.setDecisionBaselineLocked(state)
	return providers.Update{Patch: protocol.StatePatch{Weather: &state}}, true
}

func (provider *Provider) stateUpdatesLocked(now time.Time, hourlySucceeded bool) ([]providers.Update, error) {
	state, err := BuildWeatherState(now, provider.config, provider.current, provider.hourly)
	if err != nil {
		return nil, err
	}
	updates := []providers.Update{{Patch: protocol.StatePatch{Weather: &state}}}
	windowKey := outingWindowKey(state.NextOuting)
	if windowKey != provider.lastWindowKey {
		provider.lastWindowKey = windowKey
		provider.lastDecision = "unknown"
	}
	decision := decisionName(state.NextOuting.UmbrellaRequired)
	transitioned := decision == "required" && provider.lastDecision != "required"
	if transitioned {
		updates = append(updates, providers.Update{Notification: provider.umbrellaNotification(state, false, now)})
	}
	if decision == "required" && !transitioned && hourlySucceeded {
		remaining := state.NextOuting.TargetAt.Sub(now.In(state.NextOuting.TargetAt.Location()))
		if remaining >= 0 && remaining <= provider.config.Umbrella.RepeatBeforeOuting && !provider.reminded[windowKey] {
			provider.reminded[windowKey] = true
			updates = append(updates, providers.Update{Notification: provider.umbrellaNotification(state, true, now)})
		}
	}
	provider.lastDecision = decision
	stale := state.Current.Freshness == protocol.FreshnessStale || state.Lunch.Freshness == protocol.FreshnessStale || state.Leave.Freshness == protocol.FreshnessStale
	if stale && !provider.staleNotified {
		provider.staleNotified = true
		updates = append(updates, providers.Update{Notification: provider.staleNotification(now)})
	}
	if !stale {
		provider.staleNotified = false
	}
	provider.lastScheduledWindowKey = windowKey
	return updates, nil
}

func (provider *Provider) setDecisionBaselineLocked(state protocol.WeatherState) {
	provider.lastWindowKey = outingWindowKey(state.NextOuting)
	provider.lastScheduledWindowKey = provider.lastWindowKey
	provider.lastDecision = decisionName(state.NextOuting.UmbrellaRequired)
	provider.staleNotified = state.Current.Freshness == protocol.FreshnessStale || state.Lunch.Freshness == protocol.FreshnessStale || state.Leave.Freshness == protocol.FreshnessStale
}

func (provider *Provider) minuteUpdates(ctx context.Context) ([]providers.Update, error) {
	now := provider.now()
	targets, err := TargetsFor(now, provider.config.Timezone, provider.config.Schedule)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s:%s", targets.NextOuting.Format("2006-01-02"), targets.NextSlot)
	provider.mu.Lock()
	refresh := key != provider.lastScheduledWindowKey
	for _, before := range provider.config.Refresh.ForceBeforeOuting {
		forceKey := fmt.Sprintf("%s:%s", key, before)
		remaining := targets.NextOuting.Sub(now.In(targets.NextOuting.Location()))
		if remaining >= 0 && remaining <= before && !provider.forcedRefreshes[forceKey] {
			provider.forcedRefreshes[forceKey] = true
			refresh = true
		}
	}
	provider.mu.Unlock()
	if refresh {
		return provider.refresh(ctx, refreshHourly, true)
	}
	provider.mu.Lock()
	updates, stateErr := provider.stateUpdatesLocked(now, false)
	provider.mu.Unlock()
	if stateErr != nil {
		return nil, stateErr
	}
	if len(updates) == 1 && updates[0].Notification == nil {
		return nil, nil
	}
	return updates, nil
}

func (provider *Provider) umbrellaNotification(state protocol.WeatherState, reminder bool, now time.Time) *protocol.Notification {
	outing := state.NextOuting
	date := outing.TargetAt.Format("2006-01-02")
	kind := "weather.umbrella_required"
	prefix := "weather:umbrella:"
	if reminder {
		kind = "weather.umbrella_reminder"
		prefix = "weather:umbrella-reminder:"
	}
	label := "午饭"
	if outing.Slot == "leave" {
		label = "下班"
	}
	return &protocol.Notification{
		Category: protocol.CategoryWeather, Kind: kind, Source: "qweather",
		SubjectID: provider.config.Location + ":" + outing.Slot, Theme: protocol.ThemeRed,
		Urgency: protocol.UrgencyAttention, Priority: 72,
		DedupeKey: prefix + date + ":" + outing.Slot, SupersedeKey: "weather:umbrella:" + date + ":" + outing.Slot,
		Title: label + "记得带伞", Detail: outing.Reason, SourceLabel: "QWeather", DisplayMS: 6500,
		ExpiresAt: outing.TargetAt.Add(provider.config.Umbrella.WindowAfter), ReplayAfterInterrupt: true, MaxReplays: 1,
	}
}

func (provider *Provider) staleNotification(now time.Time) *protocol.Notification {
	return &protocol.Notification{
		Category: protocol.CategoryWeather, Kind: "weather.data_stale", Source: "qweather", SubjectID: provider.config.Location,
		Theme: protocol.ThemeYellow, Urgency: protocol.UrgencyNormal, Priority: 40,
		DedupeKey: "weather:data-stale:" + provider.config.Location, SupersedeKey: "weather:data-stale:" + provider.config.Location,
		Title: "天气数据已过期", Detail: "带伞判断暂不可用", SourceLabel: "QWeather", DisplayMS: 5000,
		ExpiresAt: now.Add(15 * time.Minute), MaxReplays: 0,
	}
}

func (provider *Provider) replaceNowCacheRecordLocked(data NowData) {
	filtered := provider.cacheRecords[:0]
	for _, record := range provider.cacheRecords {
		if record.Endpoint != "/v7/weather/now" || record.Location != provider.config.Location {
			filtered = append(filtered, record)
		}
	}
	provider.cacheRecords = append(filtered, CacheRecord{Provider: "qweather", Endpoint: "/v7/weather/now", Location: provider.config.Location,
		FetchedAt: data.FetchedAt, UpdateTime: data.UpdateTime.Format(time.RFC3339), PayloadJSON: append(json.RawMessage(nil), data.Raw...)})
}

func (provider *Provider) appendHourlyCacheRecordLocked(data HourlyData) {
	provider.cacheRecords = append(provider.cacheRecords, CacheRecord{Provider: "qweather", Endpoint: data.Endpoint, Location: provider.config.Location,
		FetchedAt: data.FetchedAt, UpdateTime: data.UpdateTime.Format(time.RFC3339), PayloadJSON: append(json.RawMessage(nil), data.Raw...)})
	provider.pruneCacheRecordsLocked()
}

func (provider *Provider) pruneCacheRecordsLocked() {
	sort.SliceStable(provider.cacheRecords, func(left, right int) bool {
		return provider.cacheRecords[left].FetchedAt.Before(provider.cacheRecords[right].FetchedAt)
	})
	hourlyCount := 0
	for _, record := range provider.cacheRecords {
		if strings.Contains(record.Endpoint, "/weather/24h") || strings.Contains(record.Endpoint, "/weather/72h") {
			hourlyCount++
		}
	}
	removeHourly := hourlyCount - 48
	if removeHourly <= 0 {
		return
	}
	kept := provider.cacheRecords[:0]
	for _, record := range provider.cacheRecords {
		isHourly := record.Endpoint == "/v7/weather/24h" || record.Endpoint == "/v7/weather/72h"
		if isHourly && removeHourly > 0 {
			removeHourly--
			continue
		}
		kept = append(kept, record)
	}
	provider.cacheRecords = kept
}

func parseCachedNow(record CacheRecord) (NowData, error) {
	var response nowResponse
	if err := decodeResponse(record.PayloadJSON, &response); err != nil || response.Code != "200" {
		return NowData{}, errors.New("invalid cached qweather now response")
	}
	updateTime, err := parseQWeatherTime(response.UpdateTime)
	if err != nil {
		return NowData{}, err
	}
	observedAt, err := parseQWeatherTime(response.Now.ObsTime)
	if err != nil || response.Now.Icon == "" || response.Now.Text == "" {
		return NowData{}, errors.New("invalid cached qweather observation")
	}
	return NowData{UpdateTime: updateTime, ObservedAt: observedAt, Temp: parseInt(response.Now.Temp, -80, 80), Icon: response.Now.Icon,
		Text: response.Now.Text, Precip: parseFloat(response.Now.Precip, 0), Refer: response.Refer, FetchedAt: record.FetchedAt,
		Raw: append(json.RawMessage(nil), record.PayloadJSON...), FromCache: true}, nil
}

func parseCachedHourly(record CacheRecord) (HourlyData, error) {
	var response hourlyResponse
	if err := decodeResponse(record.PayloadJSON, &response); err != nil || response.Code != "200" {
		return HourlyData{}, errors.New("invalid cached qweather hourly response")
	}
	updateTime, err := parseQWeatherTime(response.UpdateTime)
	if err != nil {
		return HourlyData{}, err
	}
	points := make([]HourlyPoint, 0, len(response.Hourly))
	for _, rawPoint := range response.Hourly {
		at, parseErr := parseQWeatherTime(rawPoint.ForecastTime)
		if parseErr == nil {
			points = append(points, HourlyPoint{ForecastAt: at, Temp: parseInt(rawPoint.Temp, -80, 80), Icon: rawPoint.Icon,
				Text: rawPoint.Text, POP: parseInt(rawPoint.POP, 0, 100), Precip: parseFloat(rawPoint.Precip, 0)})
		}
	}
	if len(points) == 0 {
		return HourlyData{}, errors.New("cached qweather hourly response has no valid forecast")
	}
	return HourlyData{Endpoint: record.Endpoint, UpdateTime: updateTime, Points: points, Refer: response.Refer,
		FetchedAt: record.FetchedAt, Raw: append(json.RawMessage(nil), record.PayloadJSON...), FromCache: true}, nil
}

func mergeHourly(existing *HourlyData, next HourlyData, now time.Time) *HourlyData {
	merged := next
	byTime := make(map[int64]HourlyPoint)
	if existing != nil {
		for _, point := range existing.Points {
			byTime[point.ForecastAt.Unix()] = point
		}
	}
	for _, point := range next.Points {
		byTime[point.ForecastAt.Unix()] = point
	}
	minimum := now.Add(-24 * time.Hour)
	maximum := now.Add(96 * time.Hour)
	merged.Points = make([]HourlyPoint, 0, len(byTime))
	for _, point := range byTime {
		if !point.ForecastAt.Before(minimum) && !point.ForecastAt.After(maximum) {
			merged.Points = append(merged.Points, point)
		}
	}
	sort.Slice(merged.Points, func(left, right int) bool {
		return merged.Points[left].ForecastAt.Before(merged.Points[right].ForecastAt)
	})
	return &merged
}

func (provider *Provider) recordFailure(err error) time.Duration {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.failures++
	delay := exponentialDelay(provider.failures)
	applyJitter := true
	var apiError *APIError
	if errors.As(err, &apiError) {
		if apiError.StatusCode == 429 && apiError.RetryAfter > 0 {
			delay = apiError.RetryAfter
			applyJitter = false
		} else if apiError.StatusCode == 403 {
			delay = 15 * time.Minute
			applyJitter = false
		}
	}
	if applyJitter {
		delay = provider.jitter(delay)
	}
	provider.nextRetryAt = provider.now().Add(delay)
	provider.lastError = err.Error()
	return delay
}

func (provider *Provider) resetFailures() {
	provider.mu.Lock()
	provider.failures = 0
	provider.nextRetryAt = time.Time{}
	provider.lastError = ""
	provider.mu.Unlock()
}

func exponentialDelay(failures int) time.Duration {
	sequence := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute}
	if failures < 1 {
		failures = 1
	}
	if failures > len(sequence) {
		failures = len(sequence)
	}
	return sequence[failures-1]
}

func defaultJitter(base time.Duration) time.Duration {
	spread := base / 10
	if spread <= 0 {
		return base
	}
	offset := time.Duration(rand.Int64N(int64(2*spread)+1)) - spread
	return base + offset
}

func outingWindowKey(outing protocol.NextOuting) string {
	return fmt.Sprintf("%s:%s", outing.TargetAt.Format("2006-01-02"), outing.Slot)
}

func decisionName(required *bool) string {
	if required == nil {
		return "unknown"
	}
	if *required {
		return "required"
	}
	return "not_required"
}

func sendProviderUpdates(ctx context.Context, output chan<- providers.Update, updates []providers.Update) error {
	for _, update := range updates {
		if err := sendProviderUpdate(ctx, output, update); err != nil {
			return err
		}
	}
	return nil
}

func sendProviderUpdate(ctx context.Context, output chan<- providers.Update, update providers.Update) error {
	select {
	case output <- update:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
