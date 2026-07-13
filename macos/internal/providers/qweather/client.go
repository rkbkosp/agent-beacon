package qweather

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxResponseBytes = 1 << 20
	userAgent        = "AgentBeacon/0.1"
)

type TokenSigner interface {
	Token(time.Time) (string, error)
	Invalidate()
}

type Refer struct {
	Sources []string `json:"sources"`
	License []string `json:"license"`
}

type NowData struct {
	UpdateTime time.Time
	ObservedAt time.Time
	Temp       *int
	Icon       string
	Text       string
	Precip     *float64
	Refer      Refer
	FetchedAt  time.Time
	Raw        json.RawMessage
	FromCache  bool
}

type HourlyPoint struct {
	ForecastAt time.Time
	Temp       *int
	Icon       string
	Text       string
	POP        *int
	Precip     *float64
}

type HourlyData struct {
	Endpoint   string
	UpdateTime time.Time
	Points     []HourlyPoint
	Refer      Refer
	FetchedAt  time.Time
	Raw        json.RawMessage
	FromCache  bool
}

type APIError struct {
	StatusCode int
	Code       string
	RetryAfter time.Duration
	Kind       string
	Err        error
}

func (apiError *APIError) Error() string {
	parts := []string{"qweather request failed"}
	if apiError.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", apiError.StatusCode))
	}
	if apiError.Code != "" {
		parts = append(parts, "API code "+apiError.Code)
	}
	if apiError.Kind != "" {
		parts = append(parts, apiError.Kind)
	}
	return strings.Join(parts, ": ")
}

func (apiError *APIError) Unwrap() error { return apiError.Err }

type inFlight struct {
	done chan struct{}
	data []byte
	err  error
}

type Client struct {
	apiHost   string
	location  string
	lang      string
	signer    TokenSigner
	http      *http.Client
	now       func() time.Time
	flightsMu sync.Mutex
	flights   map[string]*inFlight
}

func NewClient(apiHost, location, lang string, signer TokenSigner, httpClient *http.Client) (*Client, error) {
	if apiHost == "" || strings.ContainsAny(apiHost, "/:@?#") || apiHost != strings.ToLower(apiHost) ||
		!strings.HasSuffix(apiHost, ".qweatherapi.com") || strings.TrimSuffix(apiHost, ".qweatherapi.com") == "" {
		return nil, errors.New("qweather api_host must be an account-specific *.qweatherapi.com hostname without scheme or path")
	}
	if location == "" {
		return nil, errors.New("qweather location is required")
	}
	if lang == "" {
		lang = "zh"
	}
	if signer == nil {
		return nil, errors.New("qweather JWT signer is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Client{apiHost: apiHost, location: location, lang: lang, signer: signer, http: httpClient,
		now: time.Now, flights: make(map[string]*inFlight)}, nil
}

type nowResponse struct {
	Code       string `json:"code"`
	UpdateTime string `json:"updateTime"`
	Now        struct {
		ObsTime string `json:"obsTime"`
		Temp    string `json:"temp"`
		Icon    string `json:"icon"`
		Text    string `json:"text"`
		Precip  string `json:"precip"`
	} `json:"now"`
	Refer Refer `json:"refer"`
}

type hourlyResponse struct {
	Code       string `json:"code"`
	UpdateTime string `json:"updateTime"`
	Hourly     []struct {
		ForecastTime string `json:"fxTime"`
		Temp         string `json:"temp"`
		Icon         string `json:"icon"`
		Text         string `json:"text"`
		POP          string `json:"pop"`
		Precip       string `json:"precip"`
	} `json:"hourly"`
	Refer Refer `json:"refer"`
}

func (client *Client) FetchNow(ctx context.Context) (NowData, error) {
	query := url.Values{"location": []string{client.location}, "lang": []string{client.lang}}
	raw, err := client.get(ctx, "/v7/weather/now", query)
	if err != nil {
		return NowData{}, err
	}
	var response nowResponse
	if err := decodeResponse(raw, &response); err != nil {
		return NowData{}, &APIError{StatusCode: http.StatusOK, Kind: "invalid now response", Err: err}
	}
	if response.Code != "200" {
		return NowData{}, &APIError{StatusCode: http.StatusOK, Code: response.Code, Kind: "upstream API error"}
	}
	updateTime, err := parseQWeatherTime(response.UpdateTime)
	if err != nil {
		return NowData{}, &APIError{StatusCode: http.StatusOK, Kind: "invalid updateTime", Err: err}
	}
	observedAt, err := parseQWeatherTime(response.Now.ObsTime)
	if err != nil {
		return NowData{}, &APIError{StatusCode: http.StatusOK, Kind: "invalid obsTime", Err: err}
	}
	if response.Now.Icon == "" || response.Now.Text == "" {
		return NowData{}, &APIError{StatusCode: http.StatusOK, Kind: "now weather icon and text are required"}
	}
	return NowData{UpdateTime: updateTime, ObservedAt: observedAt, Temp: parseInt(response.Now.Temp, -80, 80),
		Icon: response.Now.Icon, Text: response.Now.Text, Precip: parseFloat(response.Now.Precip, 0), Refer: response.Refer,
		FetchedAt: client.now(), Raw: append(json.RawMessage(nil), raw...)}, nil
}

func (client *Client) FetchHourly(ctx context.Context, horizon time.Duration) (HourlyData, error) {
	path := "/v7/weather/24h"
	if horizon > 24*time.Hour {
		path = "/v7/weather/72h"
	}
	query := url.Values{"location": []string{client.location}, "lang": []string{client.lang}}
	raw, err := client.get(ctx, path, query)
	if err != nil {
		return HourlyData{}, err
	}
	var response hourlyResponse
	if err := decodeResponse(raw, &response); err != nil {
		return HourlyData{}, &APIError{StatusCode: http.StatusOK, Kind: "invalid hourly response", Err: err}
	}
	if response.Code != "200" {
		return HourlyData{}, &APIError{StatusCode: http.StatusOK, Code: response.Code, Kind: "upstream API error"}
	}
	updateTime, err := parseQWeatherTime(response.UpdateTime)
	if err != nil {
		return HourlyData{}, &APIError{StatusCode: http.StatusOK, Kind: "invalid updateTime", Err: err}
	}
	points := make([]HourlyPoint, 0, len(response.Hourly))
	for _, rawPoint := range response.Hourly {
		forecastAt, parseErr := parseQWeatherTime(rawPoint.ForecastTime)
		if parseErr != nil {
			continue
		}
		points = append(points, HourlyPoint{ForecastAt: forecastAt, Temp: parseInt(rawPoint.Temp, -80, 80), Icon: rawPoint.Icon,
			Text: rawPoint.Text, POP: parseInt(rawPoint.POP, 0, 100), Precip: parseFloat(rawPoint.Precip, 0)})
	}
	if len(points) == 0 {
		return HourlyData{}, &APIError{StatusCode: http.StatusOK, Kind: "hourly response has no valid forecast times"}
	}
	return HourlyData{Endpoint: path, UpdateTime: updateTime, Points: points, Refer: response.Refer,
		FetchedAt: client.now(), Raw: append(json.RawMessage(nil), raw...)}, nil
}

func (client *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	key := path + "?" + query.Encode()
	client.flightsMu.Lock()
	if existing := client.flights[key]; existing != nil {
		client.flightsMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-existing.done:
			return append([]byte(nil), existing.data...), existing.err
		}
	}
	flight := &inFlight{done: make(chan struct{})}
	client.flights[key] = flight
	client.flightsMu.Unlock()

	flight.data, flight.err = client.getWithAuthRetry(ctx, path, query)
	client.flightsMu.Lock()
	delete(client.flights, key)
	close(flight.done)
	client.flightsMu.Unlock()
	return append([]byte(nil), flight.data...), flight.err
}

func (client *Client) getWithAuthRetry(ctx context.Context, path string, query url.Values) ([]byte, error) {
	data, status, retryAfter, err := client.requestOnce(ctx, path, query)
	if status == http.StatusUnauthorized {
		client.signer.Invalidate()
		data, status, retryAfter, err = client.requestOnce(ctx, path, query)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		kind := "HTTP error"
		switch status {
		case http.StatusUnauthorized:
			kind = "authentication failed after one JWT refresh"
		case http.StatusForbidden:
			kind = "access forbidden; check api_host, permissions, account balance, and console security settings"
		case http.StatusTooManyRequests:
			kind = "rate limited"
		}
		return nil, &APIError{StatusCode: status, RetryAfter: retryAfter, Kind: kind}
	}
	return data, nil
}

func (client *Client) requestOnce(ctx context.Context, path string, query url.Values) ([]byte, int, time.Duration, error) {
	token, err := client.signer.Token(client.now())
	if err != nil {
		return nil, 0, 0, fmt.Errorf("create qweather authorization: %w", err)
	}
	requestURL := url.URL{Scheme: "https", Host: client.apiHost, Path: path, RawQuery: query.Encode()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("create qweather request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.http.Do(request)
	if err != nil {
		return nil, 0, 0, &APIError{Kind: "network error", Err: err}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, response.StatusCode, 0, &APIError{StatusCode: response.StatusCode, Kind: "read response", Err: err}
	}
	if len(data) > maxResponseBytes {
		return nil, response.StatusCode, 0, &APIError{StatusCode: response.StatusCode, Kind: "response exceeds 1 MiB limit"}
	}
	return data, response.StatusCode, parseRetryAfter(response.Header.Get("Retry-After"), client.now()), nil
}

func decodeResponse(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("response has trailing JSON data")
	}
	return nil
}

func parseQWeatherTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04Z07:00"} {
		if value, err := time.Parse(layout, raw); err == nil {
			return value, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid QWeather timestamp")
}

func parseInt(raw string, minimum, maximum int) *int {
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return nil
	}
	return &value
}

func parseFloat(raw string, minimum float64) *float64 {
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < minimum {
		return nil
	}
	return &value
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
