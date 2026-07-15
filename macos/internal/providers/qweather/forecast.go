package qweather

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
)

type Targets struct {
	Lunch      time.Time
	Leave      time.Time
	NextSlot   string
	NextOuting time.Time
}

func TargetsFor(now time.Time, timezone string, schedule config.WeatherScheduleConfig) (Targets, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Targets{}, fmt.Errorf("load qweather timezone: %w", err)
	}
	lunchHour, lunchMinute, err := parseClock(schedule.Lunch)
	if err != nil {
		return Targets{}, fmt.Errorf("parse lunch schedule: %w", err)
	}
	leaveHour, leaveMinute, err := parseClock(schedule.Leave)
	if err != nil {
		return Targets{}, fmt.Errorf("parse leave schedule: %w", err)
	}
	active := make(map[int]bool, len(schedule.ActiveWeekdays))
	for _, weekday := range schedule.ActiveWeekdays {
		active[weekday] = true
	}
	if len(active) == 0 {
		return Targets{}, errors.New("qweather active weekdays are empty")
	}
	localNow := now.In(location)
	today := startOfDay(localNow)
	todayLunch := time.Date(today.Year(), today.Month(), today.Day(), lunchHour, lunchMinute, 0, 0, location)
	todayLeave := time.Date(today.Year(), today.Month(), today.Day(), leaveHour, leaveMinute, 0, 0, location)
	activeToday := active[isoWeekday(today.Weekday())]

	displayDate := today
	if !activeToday || !localNow.Before(todayLeave) {
		displayDate, err = nextActiveDay(today.AddDate(0, 0, 1), active)
		if err != nil {
			return Targets{}, err
		}
	}
	lunch := time.Date(displayDate.Year(), displayDate.Month(), displayDate.Day(), lunchHour, lunchMinute, 0, 0, location)
	leave := time.Date(displayDate.Year(), displayDate.Month(), displayDate.Day(), leaveHour, leaveMinute, 0, 0, location)

	nextSlot := "lunch"
	nextOuting := lunch
	if activeToday && localNow.Before(todayLunch) {
		nextOuting = todayLunch
	} else if activeToday && localNow.Before(todayLeave) {
		nextSlot = "leave"
		nextOuting = todayLeave
	}
	return Targets{Lunch: lunch, Leave: leave, NextSlot: nextSlot, NextOuting: nextOuting}, nil
}

func RequiredHorizon(now time.Time, targets Targets) time.Duration {
	maximum := time.Duration(0)
	for _, target := range []time.Time{targets.Lunch, targets.Leave, targets.NextOuting} {
		if duration := target.Sub(now); duration > maximum {
			maximum = duration
		}
	}
	return maximum
}

func SelectForecast(points []HourlyPoint, target time.Time) (HourlyPoint, bool) {
	candidates := append([]HourlyPoint(nil), points...)
	sort.SliceStable(candidates, func(left, right int) bool {
		return candidates[left].ForecastAt.Before(candidates[right].ForecastAt)
	})
	bestIndex := -1
	bestDistance := time.Duration(1<<63 - 1)
	for index, point := range candidates {
		local := point.ForecastAt.In(target.Location())
		if local.Year() != target.Year() || local.YearDay() != target.YearDay() {
			continue
		}
		distance := local.Sub(target)
		if distance < 0 {
			distance = -distance
		}
		if distance == 0 {
			return point, true
		}
		if distance <= 59*time.Minute && distance < bestDistance {
			bestIndex = index
			bestDistance = distance
		}
	}
	if bestIndex < 0 {
		return HourlyPoint{}, false
	}
	return candidates[bestIndex], true
}

func BuildWeatherState(now time.Time, weather config.WeatherConfig, current *NowData, hourly *HourlyData) (protocol.WeatherState, error) {
	return BuildWeatherStateWithRadiation(now, weather, current, hourly, nil)
}

func BuildWeatherStateWithRadiation(now time.Time, weather config.WeatherConfig, current *NowData, hourly *HourlyData,
	radiation map[string]*RadiationData) (protocol.WeatherState, error) {
	targets, err := TargetsFor(now, weather.Timezone, weather.Schedule)
	if err != nil {
		return protocol.WeatherState{}, err
	}
	location, err := time.LoadLocation(weather.Timezone)
	if err != nil {
		return protocol.WeatherState{}, err
	}
	localNow := now.In(location)
	state := protocol.WeatherState{
		Location: weather.LocationLabel,
		Provider: "qweather",
		Current:  protocol.WeatherCurrent{ObservedAt: localNow, Icon: "-", Text: "暂无", Freshness: protocol.FreshnessUnknown},
		Lunch:    unavailableSlot(targets.Lunch, !localNow.Before(targets.Lunch)),
		Leave:    unavailableSlot(targets.Leave, !localNow.Before(targets.Leave)),
		NextOuting: protocol.NextOuting{Slot: targets.NextSlot, TargetAt: targets.NextOuting,
			Confidence: "unknown", Reason: "数据不足"},
		UpdatedAt: localNow,
	}
	if current != nil {
		state.Current = protocol.WeatherCurrent{ObservedAt: current.ObservedAt, TempC: valueOrZero(current.Temp), Icon: current.Icon,
			Text: current.Text, PrecipMM: floatOrZero(current.Precip), Freshness: dataFreshness(localNow, current.FetchedAt, weather.Cache.NowStaleAfter, current.FromCache)}
		if current.Temp == nil || current.Icon == "" || current.Text == "" {
			state.Current.Freshness = protocol.FreshnessUnknown
		}
		state.UpdatedAt = current.FetchedAt
	}
	decision := UmbrellaDecision{Confidence: "unknown", Reason: "数据不足"}
	if hourly != nil {
		hourlyFreshness := dataFreshness(localNow, hourly.FetchedAt, weather.Cache.HourlyStaleAfter, hourly.FromCache)
		state.Lunch = slotFromForecast(hourly.Points, targets.Lunch, !localNow.Before(targets.Lunch), hourlyFreshness)
		state.Leave = slotFromForecast(hourly.Points, targets.Leave, !localNow.Before(targets.Leave), hourlyFreshness)
		decision = DecideUmbrella(hourly.Points, targets.NextOuting, weather.Umbrella, hourlyFreshness == protocol.FreshnessStale)
		if hourly.FetchedAt.After(state.UpdatedAt) {
			state.UpdatedAt = hourly.FetchedAt
		}
	}
	if weather.Satellite.Enabled {
		radiationData := radiation[radiationWindowKey(targets.NextOuting, targets.NextSlot)]
		decision = CombineUmbrellaDecision(decision, DecideSunshade(radiationData, localNow, weather.Satellite))
		if radiationData != nil && radiationData.FetchedAt.After(state.UpdatedAt) {
			state.UpdatedAt = radiationData.FetchedAt
		}
	}
	state.NextOuting.UmbrellaRequired = decision.Required
	state.NextOuting.Confidence = decision.Confidence
	state.NextOuting.Reason = decision.Reason
	return state, nil
}

func radiationWindowKey(target time.Time, slot string) string {
	return fmt.Sprintf("%s:%s", target.Format("2006-01-02"), slot)
}

func parseClock(raw string) (int, int, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("time must use HH:MM")
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, errors.New("time must use HH:MM")
	}
	return hour, minute, nil
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func isoWeekday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}

func nextActiveDay(start time.Time, active map[int]bool) (time.Time, error) {
	for offset := 0; offset < 14; offset++ {
		candidate := start.AddDate(0, 0, offset)
		if active[isoWeekday(candidate.Weekday())] {
			return candidate, nil
		}
	}
	return time.Time{}, errors.New("no active qweather weekday found")
}

func dataFreshness(now, fetchedAt time.Time, staleAfter time.Duration, fromCache bool) protocol.Freshness {
	if fetchedAt.IsZero() {
		return protocol.FreshnessUnknown
	}
	if now.Sub(fetchedAt) > staleAfter {
		return protocol.FreshnessStale
	}
	if fromCache {
		return protocol.FreshnessCached
	}
	return protocol.FreshnessFresh
}

func unavailableSlot(target time.Time, isPast bool) protocol.WeatherSlot {
	return protocol.WeatherSlot{TargetAt: target, IsPast: isPast, Icon: "-", Text: "暂无", Freshness: protocol.FreshnessUnknown}
}

func slotFromForecast(points []HourlyPoint, target time.Time, isPast bool, freshness protocol.Freshness) protocol.WeatherSlot {
	point, ok := SelectForecast(points, target)
	if !ok {
		return unavailableSlot(target, isPast)
	}
	slot := protocol.WeatherSlot{TargetAt: target, IsPast: isPast, TempC: valueOrZero(point.Temp), Icon: point.Icon,
		Text: point.Text, POP: valueOrZero(point.POP), PrecipMM: floatOrZero(point.Precip), Freshness: freshness}
	if point.Temp == nil || (point.Icon == "" && point.Text == "") {
		slot.Freshness = protocol.FreshnessUnknown
	}
	if slot.Icon == "" {
		slot.Icon = "-"
	}
	if slot.Text == "" {
		slot.Text = "暂无"
	}
	return slot
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func floatOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
