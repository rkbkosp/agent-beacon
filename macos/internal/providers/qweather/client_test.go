package qweather

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type fakeSigner struct {
	mu          sync.Mutex
	token       string
	invalidates int
}

func (signer *fakeSigner) Token(time.Time) (string, error) {
	signer.mu.Lock()
	defer signer.mu.Unlock()
	return signer.token, nil
}

func (signer *fakeSigner) Invalidate() {
	signer.mu.Lock()
	signer.invalidates++
	signer.mu.Unlock()
}

func response(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{StatusCode: status, Status: strconv.Itoa(status), Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func testHTTPClient(transport roundTripFunc) *http.Client {
	return &http.Client{Transport: transport, Timeout: time.Second}
}

func TestClientFetchNowUsesBearerHeaderAndExpectedQuery(t *testing.T) {
	signer := &fakeSigner{token: "signed-jwt"}
	client, err := NewClient("abc.def.qweatherapi.com", "120.16,30.27", "zh", signer, testHTTPClient(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "abc.def.qweatherapi.com" || request.URL.Path != "/v7/weather/now" {
			t.Fatalf("request URL = %s %s", request.Method, request.URL)
		}
		if request.URL.Query().Get("location") != "120.16,30.27" || request.URL.Query().Get("lang") != "zh" || request.URL.Query().Has("key") {
			t.Fatalf("query = %v", request.URL.Query())
		}
		if request.Header.Get("Authorization") != "Bearer signed-jwt" || request.Header.Get("Accept") != "application/json" || request.Header.Get("User-Agent") != "AgentBeacon/0.1" {
			t.Fatalf("headers = %v", request.Header)
		}
		return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T14:20+08:00","now":{"obsTime":"2026-07-14T14:10+08:00","temp":"29","icon":"101","text":"多云","precip":"0.0"},"refer":{"sources":["QWeather"],"license":["QWeather Developers License"]}}`, nil), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Date(2026, 7, 14, 14, 21, 0, 0, time.FixedZone("CST", 8*60*60)) }
	got, err := client.FetchNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Temp == nil || *got.Temp != 29 || got.Precip == nil || *got.Precip != 0 || got.Icon != "101" || got.Text != "多云" {
		t.Fatalf("now data = %+v", got)
	}
	if got.ObservedAt.Format(time.RFC3339) != "2026-07-14T14:10:00+08:00" || len(got.Refer.Sources) != 1 || len(got.Raw) == 0 {
		t.Fatalf("now metadata = %+v", got)
	}
}

func TestClientFetchHourlySelects24And72HourPaths(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		horizon time.Duration
		path    string
	}{{"24 hours", 24 * time.Hour, "/v7/weather/24h"}, {"over 24 hours", 24*time.Hour + time.Second, "/v7/weather/72h"}} {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := NewClient("abc.qweatherapi.com", "101210101", "zh", &fakeSigner{token: "jwt"}, testHTTPClient(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != testCase.path {
					t.Fatalf("path = %q", request.URL.Path)
				}
				return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T14:00+08:00","hourly":[{"fxTime":"2026-07-14T15:00+08:00","temp":"30","icon":"100","text":"晴","pop":"10","precip":"0.0"}],"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			got, fetchErr := client.FetchHourly(context.Background(), testCase.horizon)
			if fetchErr != nil {
				t.Fatal(fetchErr)
			}
			if got.Endpoint != testCase.path || len(got.Points) != 1 || got.Points[0].POP == nil || *got.Points[0].POP != 10 {
				t.Fatalf("hourly data = %+v", got)
			}
		})
	}
}

func TestClientRetries401OnceWithInvalidatedJWT(t *testing.T) {
	signer := &fakeSigner{token: "jwt"}
	requests := 0
	client, err := NewClient("abc.qweatherapi.com", "101210101", "zh", signer, testHTTPClient(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return response(http.StatusUnauthorized, `{"code":"401"}`, nil), nil
		}
		return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T14:20+08:00","now":{"obsTime":"2026-07-14T14:10+08:00","temp":"29","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || signer.invalidates != 1 {
		t.Fatalf("requests=%d invalidates=%d", requests, signer.invalidates)
	}
}

func TestClientClassifiesHTTPAndAPICodeErrors(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name       string
		status     int
		body       string
		header     http.Header
		wantStatus int
		wantCode   string
		wantRetry  time.Duration
	}{{"forbidden", 403, `{"code":"403"}`, nil, 403, "", 0},
		{"rate limited", 429, `{"code":"429"}`, http.Header{"Retry-After": []string{"120"}}, 429, "", 2 * time.Minute},
		{"server error", 503, `temporary upstream failure`, nil, 503, "", 0},
		{"API code", 200, `{"code":"204","updateTime":"2026-07-14T14:20+08:00","now":{}}`, nil, 200, "204", 0}} {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := NewClient("abc.qweatherapi.com", "101210101", "zh", &fakeSigner{token: "jwt"}, testHTTPClient(func(*http.Request) (*http.Response, error) {
				return response(testCase.status, testCase.body, testCase.header), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			client.now = func() time.Time { return now }
			_, fetchErr := client.FetchNow(context.Background())
			var apiErr *APIError
			if !errors.As(fetchErr, &apiErr) {
				t.Fatalf("error type = %T, %v", fetchErr, fetchErr)
			}
			if apiErr.StatusCode != testCase.wantStatus || apiErr.Code != testCase.wantCode || apiErr.RetryAfter != testCase.wantRetry {
				t.Fatalf("API error = %+v", apiErr)
			}
			if strings.Contains(apiErr.Error(), "temporary upstream failure") {
				t.Fatal("API error exposed upstream response body")
			}
		})
	}
}

func TestClientRejectsOversizedOrStructurallyInvalidResponse(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{{"oversized", strings.Repeat("x", maxResponseBytes+1)},
		{"missing observation time", `{"code":"200","updateTime":"2026-07-14T14:20+08:00","now":{"temp":"29","icon":"101","text":"多云","precip":"0"},"refer":{"sources":["QWeather"],"license":["license"]}}`}} {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := NewClient("abc.qweatherapi.com", "101210101", "zh", &fakeSigner{token: "jwt"}, testHTTPClient(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, testCase.body, nil), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if _, fetchErr := client.FetchNow(context.Background()); fetchErr == nil {
				t.Fatal("invalid response was accepted")
			}
		})
	}
}

func TestClientKeepsInvalidNumericFieldsUnavailable(t *testing.T) {
	client, err := NewClient("abc.qweatherapi.com", "101210101", "zh", &fakeSigner{token: "jwt"}, testHTTPClient(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"code":"200","updateTime":"2026-07-14T14:20+08:00","now":{"obsTime":"2026-07-14T14:10+08:00","temp":"hot","icon":"101","text":"多云","precip":""},"refer":{"sources":["QWeather"],"license":["license"]}}`, nil), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.FetchNow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Temp != nil || got.Precip != nil {
		t.Fatalf("invalid numeric fields = temp:%v precip:%v", got.Temp, got.Precip)
	}
}
