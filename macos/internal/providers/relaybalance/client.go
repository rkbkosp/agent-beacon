package relaybalance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 64 * 1024

var ErrInvalidCredentials = errors.New("0-0 API credentials are invalid")

type SecretReader func(context.Context, string) (string, error)

type Config struct {
	Endpoint   string
	SecretName string
	Timeout    time.Duration
}

type Client struct {
	config  Config
	http    *http.Client
	secrets SecretReader
}

type Result struct {
	Remaining float64
	Unit      string
	IsValid   bool
	FetchedAt time.Time
	Mode      string
	PlanName  string
}

type responsePayload struct {
	Balance   *float64 `json:"balance"`
	IsValid   *bool    `json:"isValid"`
	Mode      string   `json:"mode"`
	PlanName  string   `json:"planName"`
	Remaining *float64 `json:"remaining"`
	Unit      string   `json:"unit"`
}

func New(config Config, httpClient *http.Client, secretReader SecretReader) (*Client, error) {
	if config.Endpoint != "https://api.0-0.pro/v1/usage" {
		return nil, errors.New("relay endpoint must be https://api.0-0.pro/v1/usage")
	}
	if config.SecretName == "" || secretReader == nil {
		return nil, errors.New("relay secret name and reader are required")
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	}
	return &Client{config: config, http: httpClient, secrets: secretReader}, nil
}

func (client *Client) Fetch(ctx context.Context) (Result, error) {
	secret, err := client.secrets(ctx, client.config.SecretName)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.config.Endpoint, nil)
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("request 0-0 usage: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return Result{IsValid: false, FetchedAt: time.Now()}, ErrInvalidCredentials
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return Result{}, fmt.Errorf("request 0-0 usage returned %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	var payload responsePayload
	if err := decoder.Decode(&payload); err != nil {
		return Result{}, fmt.Errorf("decode 0-0 usage: %w", err)
	}
	if payload.IsValid == nil {
		return Result{}, errors.New("decode 0-0 usage: isValid is missing")
	}
	if !*payload.IsValid {
		return Result{IsValid: false, FetchedAt: time.Now()}, ErrInvalidCredentials
	}
	remaining := payload.Remaining
	if remaining == nil {
		remaining = payload.Balance
	}
	if remaining == nil || *remaining < 0 {
		return Result{}, errors.New("decode 0-0 usage: remaining and balance are missing or invalid")
	}
	unit := strings.TrimSpace(payload.Unit)
	if unit == "" {
		return Result{}, errors.New("decode 0-0 usage: unit is missing")
	}
	return Result{
		Remaining: *remaining, Unit: unit, IsValid: true, FetchedAt: time.Now(),
		Mode: payload.Mode, PlanName: payload.PlanName,
	}, nil
}
