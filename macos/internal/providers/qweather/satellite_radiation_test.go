package qweather

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-beacon/internal/config"
)

const satelliteFixture = `{
  "latitude":30.2,"longitude":120.15,"timezone":"Asia/Shanghai",
  "hourly":{
    "time":["2026-07-15T11:00","2026-07-15T11:10","2026-07-15T11:20","2026-07-15T11:30","2026-07-15T11:40"],
    "shortwave_radiation_instant":[680,725,701,null,null],
    "direct_radiation_instant":[490,535,502,null,null],
    "diffuse_radiation_instant":[190,190,199,null,null],
    "direct_normal_irradiance_instant":[540,585,550,null,null],
    "terrestrial_radiation_instant":[1050,1070,1080,1090,1095]
  }
}`

func TestSatelliteClientRequestsNativeHimawariRadiationAndUsesLastThreeCompletePoints(t *testing.T) {
	client, err := NewSatelliteClient(30.2163, 120.1734, "Asia/Shanghai", testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Scheme != "https" ||
			request.URL.Host != "satellite-api.open-meteo.com" || request.URL.Path != "/v1/archive" {
			t.Fatalf("request URL = %s", request.URL)
		}
		query := request.URL.Query()
		for key, want := range map[string]string{
			"latitude": "30.2163", "longitude": "120.1734", "timezone": "Asia/Shanghai",
			"temporal_resolution": "native", "models": "satellite_radiation_seamless", "forecast_days": "1",
			"hourly": radiationVariables,
		} {
			if got := query.Get(key); got != want {
				t.Fatalf("query %s = %q, want %q", key, got, want)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(satelliteFixture))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return shanghaiTime(t, 2026, time.July, 15, 11, 57) }
	got, err := client.FetchRadiation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := shanghaiTime(t, 2026, time.July, 15, 11, 20); !got.ObservedAt.Equal(want) {
		t.Fatalf("observed_at = %s, want %s", got.ObservedAt, want)
	}
	if got.GHI != 701 || got.Direct != 502 || got.Diffuse != 190 || got.DNI != 550 || got.Terrestrial != 1070 {
		t.Fatalf("radiation medians = %+v", got)
	}
	if got.DirectShare < 0.716 || got.DirectShare > 0.717 {
		t.Fatalf("direct share = %.4f", got.DirectShare)
	}
}

func TestParseRadiationResponseRejectsFewerThanThreeCompletePoints(t *testing.T) {
	raw := strings.Replace(satelliteFixture, "[680,725,701,null,null]", "[680,725,null,null,null]", 1)
	if _, err := parseRadiationResponse([]byte(raw), "Asia/Shanghai", time.Now()); err == nil {
		t.Fatal("response with fewer than three complete points must fail")
	}
}

func TestDecideSunshadeThresholdsAndStaleness(t *testing.T) {
	satellite := config.Default().Providers.Weather.Satellite
	now := shanghaiTime(t, 2026, time.July, 15, 11, 57)
	tests := []struct {
		name       string
		data       RadiationData
		available  bool
		required   bool
		confidence string
	}{
		{name: "strong direct", data: RadiationData{ObservedAt: now.Add(-30 * time.Minute), GHI: 500, Direct: 300, DirectShare: 0.6}, available: true, required: true, confidence: "high"},
		{name: "high GHI share", data: RadiationData{ObservedAt: now.Add(-30 * time.Minute), GHI: 560, Direct: 196, DirectShare: 0.35}, available: true, required: true, confidence: "high"},
		{name: "intermittent direct", data: RadiationData{ObservedAt: now.Add(-30 * time.Minute), GHI: 500, Direct: 200, DirectShare: 0.4}, available: true, required: true, confidence: "medium"},
		{name: "diffuse low", data: RadiationData{ObservedAt: now.Add(-30 * time.Minute), GHI: 399, Direct: 149, DirectShare: 0.37}, available: true, confidence: "high"},
		{name: "stale", data: RadiationData{ObservedAt: now.Add(-76 * time.Minute), GHI: 800, Direct: 600, DirectShare: 0.75}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DecideSunshade(&test.data, now, satellite)
			if got.Available != test.available || got.Required != test.required || got.Confidence != test.confidence {
				t.Fatalf("decision = %+v", got)
			}
		})
	}
}

func TestFreshSunshadeCanRequireUmbrellaWhenRainForecastIsUnavailable(t *testing.T) {
	weather := testWeatherConfig()
	weather.Satellite.Enabled = true
	now := shanghaiTime(t, 2026, time.July, 15, 11, 57)
	target := shanghaiTime(t, 2026, time.July, 15, 12, 0)
	radiation := map[string]*RadiationData{
		radiationWindowKey(target, "lunch"): {
			ObservedAt: now.Add(-30 * time.Minute), FetchedAt: now, GHI: 700, Direct: 500, DirectShare: 500.0 / 700.0,
		},
	}
	state, err := BuildWeatherStateWithRadiation(now, weather, nil, nil, radiation)
	if err != nil {
		t.Fatal(err)
	}
	if state.NextOuting.UmbrellaRequired == nil || !*state.NextOuting.UmbrellaRequired ||
		state.NextOuting.Reason != "遮阳" || state.NextOuting.Confidence != "high" {
		t.Fatalf("sunshade-only state = %+v", state.NextOuting)
	}
}
