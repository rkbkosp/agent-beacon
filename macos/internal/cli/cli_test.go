package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-beacon/internal/api"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/state"
)

func TestEmitPostsNamedFixture(t *testing.T) {
	store := state.NewStore(time.Minute, 100)
	server := httptest.NewServer(api.NewServer(store, api.DefaultSnapshot(), "test-token").Handler())
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{
		"emit", "--server", server.URL, "--token", "test-token", "--fixture", "herdr-blocked",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	events := store.Events(10)
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	notification, err := protocol.DecodePayload[protocol.Notification](events[0])
	if err != nil {
		t.Fatal(err)
	}
	if notification.Kind != "agent.blocked" || notification.Urgency != protocol.UrgencyAttention {
		t.Fatalf("notification = %+v", notification)
	}
}

type cliRoundTripFunc func(*http.Request) (*http.Response, error)

func (function cliRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func writeWeatherCLIConfig(t *testing.T) (string, string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "private.pem")
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := `
providers:
  weather:
    enabled: true
    provider: qweather
    api_host: abc.qweatherapi.com
    project_id: project
    credential_id: credential
    private_key_path: "` + keyPath + `"
    location: "101210101"
    location_label: "杭州"
    satellite_radiation:
      enabled: true
      latitude: 30.2163
      longitude: 120.1734
`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, string(keyPEM)
}

func TestWeatherFetchNowCommandUsesDynamicJWTWithoutPrintingSecrets(t *testing.T) {
	configPath, privatePEM := writeWeatherCLIConfig(t)
	previousFactory := weatherHTTPClientFactory
	weatherHTTPClientFactory = func(time.Duration) *http.Client {
		return &http.Client{Transport: cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
				t.Fatal("weather request did not use Bearer JWT")
			}
			body := `{"code":"200","updateTime":"2026-07-14T10:00+08:00","now":{"obsTime":"2026-07-14T09:50+08:00","temp":"29","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
	}
	t.Cleanup(func() { weatherHTTPClientFactory = previousFactory })
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"weather", "fetch-now", "--config", configPath}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"temp": "29"`) || !strings.Contains(stdout.String(), "QWeather") {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), privatePEM) || strings.Contains(strings.ToLower(stdout.String()+stderr.String()), "authorization") {
		t.Fatal("weather CLI exposed credential material")
	}
}

func TestWeatherCacheClearDoesNotRequireBridgeToken(t *testing.T) {
	configPath, _ := writeWeatherCLIConfig(t)
	t.Setenv("AGENT_BEACON_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"weather", "cache", "clear", "--config", configPath}, &stdout, &stderr)
	if exitCode != 0 || !strings.Contains(stdout.String(), "cleared") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestWeatherFetchRadiationUsesOpenMeteoNativeResolution(t *testing.T) {
	configPath, _ := writeWeatherCLIConfig(t)
	previousFactory := weatherHTTPClientFactory
	weatherHTTPClientFactory = func(time.Duration) *http.Client {
		return &http.Client{Transport: cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "satellite-api.open-meteo.com" || request.URL.Query().Get("temporal_resolution") != "native" {
				t.Fatalf("satellite request = %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"timezone":"Asia/Shanghai","hourly":{"time":["2026-07-15T11:00","2026-07-15T11:10","2026-07-15T11:20"],"shortwave_radiation_instant":[680,725,701],"direct_radiation_instant":[490,535,502],"diffuse_radiation_instant":[190,190,199],"direct_normal_irradiance_instant":[540,585,550],"terrestrial_radiation_instant":[1050,1070,1080]}}`))}, nil
		})}
	}
	t.Cleanup(func() { weatherHTTPClientFactory = previousFactory })
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"weather", "fetch-radiation", "--config", configPath}, &stdout, &stderr)
	if exitCode != 0 || !strings.Contains(stdout.String(), `"shortwave_radiation_instant"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestEmitRequiresFixture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run(context.Background(), []string{"emit", "--token", "test-token"}, &stdout, &stderr)
	if exitCode != 2 || stderr.Len() == 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}
