package qweather

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-beacon/internal/config"
)

type UmbrellaDecision struct {
	Required   *bool
	Confidence string
	Reason     string
}

func DecideUmbrella(points []HourlyPoint, target time.Time, weather config.WeatherUmbrellaConfig, stale bool) UmbrellaDecision {
	unknown := UmbrellaDecision{Confidence: "unknown", Reason: "天气数据暂不可用"}
	if stale {
		return unknown
	}
	windowStart := target.Add(-weather.WindowBefore)
	windowEnd := target.Add(weather.WindowAfter)
	candidates := make([]HourlyPoint, 0, len(points))
	for _, point := range points {
		if !point.ForecastAt.Before(windowStart) && !point.ForecastAt.After(windowEnd) {
			candidates = append(candidates, point)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		return candidates[left].ForecastAt.Before(candidates[right].ForecastAt)
	})
	if len(candidates) == 0 {
		return unknown
	}
	hasEvidence := false
	maxPOP := -1
	for _, point := range candidates {
		if point.Precip != nil || point.POP != nil || point.Icon != "" || point.Text != "" {
			hasEvidence = true
		}
		if (point.Precip != nil && *point.Precip > 0) || wetIcon(point.Icon) || wetText(point.Text) {
			required := true
			reasonText := "降水"
			if wetText(point.Text) {
				reasonText = point.Text
			}
			return UmbrellaDecision{Required: &required, Confidence: "high",
				Reason: fmt.Sprintf("%s 前后有%s", target.Format("15:04"), reasonText)}
		}
		if point.POP != nil && *point.POP > maxPOP {
			maxPOP = *point.POP
		}
	}
	if !hasEvidence {
		return unknown
	}
	if maxPOP >= weather.POPThreshold {
		required := true
		return UmbrellaDecision{Required: &required, Confidence: "medium",
			Reason: fmt.Sprintf("%s 降水概率 %d%%", target.Format("15:04"), maxPOP)}
	}
	required := false
	return UmbrellaDecision{Required: &required, Confidence: "high",
		Reason: fmt.Sprintf("%s 前后暂未见降水", target.Format("15:04"))}
}

func wetIcon(raw string) bool {
	icon, err := strconv.Atoi(raw)
	if err != nil {
		return false
	}
	return (icon >= 300 && icon <= 318) || (icon >= 350 && icon <= 351) || icon == 399 ||
		(icon >= 404 && icon <= 406) || icon == 456
}

func wetText(text string) bool {
	for _, keyword := range []string{"雨夹雪", "冻雨", "冰雹", "雨", "雷"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
