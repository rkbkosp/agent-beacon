package qweather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"agent-beacon/internal/config"
)

const (
	satelliteRadiationURL = "https://satellite-api.open-meteo.com/v1/archive"
	radiationVariables    = "shortwave_radiation_instant,direct_radiation_instant,diffuse_radiation_instant,direct_normal_irradiance_instant,terrestrial_radiation_instant"
)

type RadiationClient interface {
	FetchRadiation(context.Context) (RadiationData, error)
}

type RadiationData struct {
	ObservedAt  time.Time
	GHI         float64
	Direct      float64
	Diffuse     float64
	DNI         float64
	Terrestrial float64
	DirectShare float64
	FetchedAt   time.Time
	Slot        string
	TargetAt    time.Time
	Raw         json.RawMessage
	FromCache   bool
}

type SatelliteClient struct {
	latitude  float64
	longitude float64
	timezone  string
	http      *http.Client
	now       func() time.Time
}

func NewSatelliteClient(latitude, longitude float64, timezone string, httpClient *http.Client) (*SatelliteClient, error) {
	if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return nil, errors.New("open-meteo latitude/longitude are outside valid ranges")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, fmt.Errorf("open-meteo timezone is invalid: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &SatelliteClient{latitude: latitude, longitude: longitude, timezone: timezone, http: httpClient, now: time.Now}, nil
}

type satelliteResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Hourly    struct {
		Time        []string   `json:"time"`
		GHI         []*float64 `json:"shortwave_radiation_instant"`
		Direct      []*float64 `json:"direct_radiation_instant"`
		Diffuse     []*float64 `json:"diffuse_radiation_instant"`
		DNI         []*float64 `json:"direct_normal_irradiance_instant"`
		Terrestrial []*float64 `json:"terrestrial_radiation_instant"`
	} `json:"hourly"`
}

func (client *SatelliteClient) FetchRadiation(ctx context.Context) (RadiationData, error) {
	query := url.Values{
		"latitude":            []string{fmt.Sprintf("%.4f", client.latitude)},
		"longitude":           []string{fmt.Sprintf("%.4f", client.longitude)},
		"hourly":              []string{radiationVariables},
		"timezone":            []string{client.timezone},
		"temporal_resolution": []string{"native"},
		"models":              []string{"satellite_radiation_seamless"},
		"forecast_days":       []string{"1"},
	}
	requestURL, err := url.Parse(satelliteRadiationURL)
	if err != nil {
		return RadiationData{}, err
	}
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return RadiationData{}, fmt.Errorf("create open-meteo satellite request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.http.Do(request)
	if err != nil {
		return RadiationData{}, fmt.Errorf("open-meteo satellite network error: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return RadiationData{}, fmt.Errorf("read open-meteo satellite response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return RadiationData{}, errors.New("open-meteo satellite response exceeds 1 MiB limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return RadiationData{}, fmt.Errorf("open-meteo satellite request failed: HTTP %d", response.StatusCode)
	}
	data, err := parseRadiationResponse(raw, client.timezone, client.now())
	if err != nil {
		return RadiationData{}, fmt.Errorf("invalid open-meteo satellite response: %w", err)
	}
	return data, nil
}

func parseRadiationResponse(raw []byte, timezone string, fetchedAt time.Time) (RadiationData, error) {
	var response satelliteResponse
	if err := decodeResponse(raw, &response); err != nil {
		return RadiationData{}, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return RadiationData{}, err
	}
	length := len(response.Hourly.Time)
	if length == 0 || len(response.Hourly.GHI) != length || len(response.Hourly.Direct) != length ||
		len(response.Hourly.Diffuse) != length || len(response.Hourly.DNI) != length || len(response.Hourly.Terrestrial) != length {
		return RadiationData{}, errors.New("hourly radiation arrays must be non-empty and have matching lengths")
	}
	indices := make([]int, 0, 3)
	var observedAt time.Time
	for index := length - 1; index >= 0 && len(indices) < 3; index-- {
		if response.Hourly.GHI[index] == nil || response.Hourly.Direct[index] == nil {
			continue
		}
		at, parseErr := parseSatelliteTime(response.Hourly.Time[index], location)
		if parseErr != nil {
			continue
		}
		if observedAt.IsZero() {
			observedAt = at
		}
		indices = append(indices, index)
	}
	if len(indices) < 3 {
		return RadiationData{}, errors.New("fewer than three complete GHI/direct observations")
	}
	ghi := medianPointers(response.Hourly.GHI, indices)
	direct := medianPointers(response.Hourly.Direct, indices)
	data := RadiationData{
		ObservedAt: observedAt, GHI: ghi, Direct: direct,
		Diffuse: medianPointers(response.Hourly.Diffuse, indices), DNI: medianPointers(response.Hourly.DNI, indices),
		Terrestrial: medianPointers(response.Hourly.Terrestrial, indices), FetchedAt: fetchedAt,
		Raw: append(json.RawMessage(nil), raw...),
	}
	if data.GHI > 0 {
		data.DirectShare = data.Direct / data.GHI
	}
	return data, nil
}

func parseSatelliteTime(raw string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		var value time.Time
		var err error
		if strings.Contains(layout, "Z07:00") {
			value, err = time.Parse(layout, raw)
		} else {
			value, err = time.ParseInLocation(layout, raw, location)
		}
		if err == nil {
			return value, nil
		}
	}
	return time.Time{}, errors.New("invalid satellite timestamp")
}

func medianPointers(values []*float64, indices []int) float64 {
	selected := make([]float64, 0, len(indices))
	for _, index := range indices {
		if values[index] != nil {
			selected = append(selected, *values[index])
		}
	}
	if len(selected) == 0 {
		return 0
	}
	sort.Float64s(selected)
	return selected[len(selected)/2]
}

type SunshadeDecision struct {
	Available  bool
	Required   bool
	Confidence string
}

func DecideSunshade(data *RadiationData, now time.Time, satellite config.SatelliteRadiationConfig) SunshadeDecision {
	if data == nil || data.ObservedAt.IsZero() {
		return SunshadeDecision{}
	}
	age := now.Sub(data.ObservedAt)
	if age < -5*time.Minute || age > satellite.StaleAfter {
		return SunshadeDecision{}
	}
	if data.Direct >= satellite.DirectRequired ||
		(data.GHI >= satellite.GHIRequired && data.DirectShare >= satellite.RequiredDirectShare) {
		return SunshadeDecision{Available: true, Required: true, Confidence: "high"}
	}
	if data.Direct >= satellite.DirectSuggested ||
		(data.GHI >= satellite.GHISuggested && data.DirectShare >= satellite.SuggestedDirectShare) {
		return SunshadeDecision{Available: true, Required: true, Confidence: "medium"}
	}
	return SunshadeDecision{Available: true, Confidence: "high"}
}
