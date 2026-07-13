package qweather

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"agent-beacon/internal/config"
)

func doctorFixture(t *testing.T, mode os.FileMode, now time.Time) (config.WeatherConfig, *Client) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPath, _ := writePKCS8Key(t, private)
	if err := os.Chmod(keyPath, mode); err != nil {
		t.Fatal(err)
	}
	weather := testWeatherConfig()
	weather.PrivateKeyPath = keyPath
	client, err := NewClient(weather.APIHost, weather.Location, weather.Lang, &fakeSigner{token: "doctor-jwt"}, testHTTPClient(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v7/weather/now":
			return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T10:00+08:00","now":{"obsTime":"2026-07-14T09:50+08:00","temp":"29","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
		case "/v7/weather/24h", "/v7/weather/72h":
			return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T10:00+08:00","hourly":[{"fxTime":"2026-07-14T12:00+08:00","temp":"30","icon":"100","text":"晴","pop":"10","precip":"0"},{"fxTime":"2026-07-14T19:00+08:00","temp":"27","icon":"101","text":"多云","pop":"20","precip":"0"}],"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
		default:
			t.Fatalf("unexpected doctor path %s", request.URL.Path)
			return nil, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return now }
	return weather, client
}

func TestDoctorChecksCredentialsConnectivityAPIsTargetsAndSourcesWithoutSecrets(t *testing.T) {
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	weather, client := doctorFixture(t, 0o600, now)
	checks := Doctor(context.Background(), weather, client,
		WithDoctorClock(func() time.Time { return now }),
		WithDoctorResolver(func(context.Context, string) ([]string, error) { return []string{"203.0.113.10"}, nil }),
		WithDoctorTLS(func(context.Context, string) error { return nil }))
	if len(checks) < 10 {
		t.Fatalf("doctor checks = %+v", checks)
	}
	for _, check := range checks {
		if !check.OK {
			t.Fatalf("doctor check failed: %+v", check)
		}
	}
	encoded := strings.ToLower(formatChecks(checks))
	keyData, err := os.ReadFile(weather.PrivateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"doctor-jwt", "authorization: bearer", strings.ToLower(string(keyData))} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("doctor output exposed secret material %q", forbidden)
		}
	}
}

func TestDoctorAfterLunchOnlyRequiresForecastForFutureLeaveTarget(t *testing.T) {
	now := shanghaiTime(t, 2026, time.July, 14, 16, 45)
	weather, client := doctorFixture(t, 0o600, now)
	client.http = testHTTPClient(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v7/weather/now":
			return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T16:30+08:00","now":{"obsTime":"2026-07-14T16:25+08:00","temp":"33","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
		case "/v7/weather/24h":
			return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T16:30+08:00","hourly":[{"fxTime":"2026-07-14T19:00+08:00","temp":"30","icon":"101","text":"多云","pop":"20","precip":"0"}],"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
		default:
			t.Fatalf("unexpected doctor path %s", request.URL.Path)
			return nil, nil
		}
	})
	checks := Doctor(context.Background(), weather, client,
		WithDoctorClock(func() time.Time { return now }),
		WithDoctorResolver(func(context.Context, string) ([]string, error) { return []string{"203.0.113.10"}, nil }),
		WithDoctorTLS(func(context.Context, string) error { return nil }))
	for _, check := range checks {
		if check.Name == "forecast_targets" && !check.OK {
			t.Fatalf("past lunch must not make live forecast diagnostics fail: %+v", check)
		}
	}
}

func TestDoctorRejectsPrivateKeyPermissionsWiderThan0600(t *testing.T) {
	now := shanghaiTime(t, 2026, time.July, 14, 10, 0)
	weather, client := doctorFixture(t, 0o644, now)
	checks := Doctor(context.Background(), weather, client,
		WithDoctorClock(func() time.Time { return now }),
		WithDoctorResolver(func(context.Context, string) ([]string, error) { return []string{"203.0.113.10"}, nil }),
		WithDoctorTLS(func(context.Context, string) error { return nil }))
	found := false
	for _, check := range checks {
		if check.Name == "private_key_permissions" {
			found = true
			if check.OK || !strings.Contains(check.Detail, "chmod 600") {
				t.Fatalf("permission check = %+v", check)
			}
		}
	}
	if !found {
		t.Fatal("private key permission check missing")
	}
}
