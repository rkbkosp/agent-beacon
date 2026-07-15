package qweather

import (
	"testing"
	"time"

	"agent-beacon/internal/config"
)

func intValue(value int) *int           { return &value }
func floatValue(value float64) *float64 { return &value }
func boolValue(value bool) *bool        { return &value }

func TestUmbrellaRequiredByPrecipPOPWetIconsAndChineseText(t *testing.T) {
	target := shanghaiTime(t, 2026, time.July, 14, 19, 0)
	cfg := config.Default().Providers.Weather.Umbrella
	tests := []struct {
		name       string
		point      HourlyPoint
		confidence string
	}{{"precipitation", HourlyPoint{ForecastAt: target, Text: "多云", Icon: "101", Precip: floatValue(0.1)}, "high"},
		{"POP threshold", HourlyPoint{ForecastAt: target, Text: "多云", Icon: "101", POP: intValue(40), Precip: floatValue(0)}, "medium"},
		{"day rain lower", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "300"}, "high"},
		{"day rain upper", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "318"}, "high"},
		{"night rain", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "351"}, "high"},
		{"rain unknown", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "399"}, "high"},
		{"sleet", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "406"}, "high"},
		{"night sleet", HourlyPoint{ForecastAt: target, Text: "天气", Icon: "456"}, "high"},
		{"rain text", HourlyPoint{ForecastAt: target, Text: "局地有雨", Icon: "999"}, "high"},
		{"thunder text", HourlyPoint{ForecastAt: target, Text: "雷暴", Icon: "999"}, "high"},
		{"hail text", HourlyPoint{ForecastAt: target, Text: "冰雹", Icon: "999"}, "high"},
		{"sleet text", HourlyPoint{ForecastAt: target, Text: "雨夹雪", Icon: "999"}, "high"},
		{"freezing rain text", HourlyPoint{ForecastAt: target, Text: "冻雨", Icon: "999"}, "high"}}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := DecideUmbrella([]HourlyPoint{testCase.point}, target, cfg, false)
			if got.Required == nil || !*got.Required || got.Confidence != testCase.confidence || got.Reason != "有雨" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestUmbrellaUnknownForStaleMissingOrInvalidWindowData(t *testing.T) {
	target := shanghaiTime(t, 2026, time.July, 14, 19, 0)
	cfg := config.Default().Providers.Weather.Umbrella
	for _, testCase := range []struct {
		name   string
		points []HourlyPoint
		stale  bool
	}{{"stale", []HourlyPoint{{ForecastAt: target, Text: "晴", Icon: "100"}}, true},
		{"missing", nil, false},
		{"outside window", []HourlyPoint{{ForecastAt: target.Add(61 * time.Minute), Text: "雨", Icon: "300"}}, false},
		{"all fields invalid", []HourlyPoint{{ForecastAt: target}}, false}} {
		t.Run(testCase.name, func(t *testing.T) {
			got := DecideUmbrella(testCase.points, target, cfg, testCase.stale)
			if got.Required != nil || got.Confidence != "unknown" || got.Reason != "数据不足" {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestUmbrellaNotRequiredOnlyFromFreshValidDryWindow(t *testing.T) {
	target := shanghaiTime(t, 2026, time.July, 14, 12, 0)
	zeroPrecip := 0.0
	lowPOP := 39
	got := DecideUmbrella([]HourlyPoint{{ForecastAt: target.Add(-time.Hour), Text: "晴", Icon: "100", POP: &lowPOP, Precip: &zeroPrecip},
		{ForecastAt: target.Add(time.Hour), Text: "多云", Icon: "101", POP: intValue(10), Precip: floatValue(0)}},
		target, config.Default().Providers.Weather.Umbrella, false)
	if got.Required == nil || *got.Required || got.Confidence != "high" || got.Reason != "无雨" {
		t.Fatalf("decision = %+v", got)
	}
}

func TestCombinedUmbrellaDecisionPrioritizesRainThenSunshade(t *testing.T) {
	rainRequired := true
	rain := UmbrellaDecision{Required: &rainRequired, Confidence: "medium", Reason: "有雨"}
	got := CombineUmbrellaDecision(rain, SunshadeDecision{Available: true, Required: true, Confidence: "high"})
	if got.Required == nil || !*got.Required || got.Reason != "有雨" || got.Confidence != "medium" {
		t.Fatalf("rain priority decision = %+v", got)
	}

	unknown := UmbrellaDecision{Confidence: "unknown", Reason: "数据不足"}
	got = CombineUmbrellaDecision(unknown, SunshadeDecision{Available: true, Required: true, Confidence: "high"})
	if got.Required == nil || !*got.Required || got.Reason != "遮阳" || got.Confidence != "high" {
		t.Fatalf("sunshade fallback decision = %+v", got)
	}

	got = CombineUmbrellaDecision(unknown, SunshadeDecision{Available: true, Confidence: "high"})
	if got.Required != nil || got.Reason != "数据不足" {
		t.Fatalf("unknown rain must not become no-umbrella: %+v", got)
	}
}
