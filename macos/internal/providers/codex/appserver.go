package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxAppServerLineBytes = 4 * 1024 * 1024

type AdapterOutput struct {
	SchemaVersion int    `json:"schema_version"`
	HomeID        string `json:"home_id"`
	Weekly        struct {
		RemainingPercent int        `json:"remaining_percent"`
		ResetAt          *time.Time `json:"reset_at"`
	} `json:"weekly"`
	ResetCards struct {
		Available        *int       `json:"available"`
		NearestExpiresAt *time.Time `json:"nearest_expires_at"`
	} `json:"reset_cards"`
	ObservedAt time.Time `json:"observed_at"`
}

type appServerWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type appServerRateLimits struct {
	LimitID   *string          `json:"limitId"`
	Primary   *appServerWindow `json:"primary"`
	Secondary *appServerWindow `json:"secondary"`
}

type appServerResetCredit struct {
	ExpiresAt *int64 `json:"expiresAt"`
	Status    string `json:"status"`
}

type appServerRateResponse struct {
	RateLimits            appServerRateLimits            `json:"rateLimits"`
	RateLimitsByLimitID   map[string]appServerRateLimits `json:"rateLimitsByLimitId"`
	RateLimitResetCredits *struct {
		AvailableCount int                    `json:"availableCount"`
		Credits        []appServerResetCredit `json:"credits"`
	} `json:"rateLimitResetCredits"`
}

type appServerMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *appServerError `json:"error"`
}

type appServerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ReadAppServerQuota(ctx context.Context, homeID string) (AdapterOutput, error) {
	homeID = strings.TrimSpace(homeID)
	if homeID == "" {
		return AdapterOutput{}, errors.New("Codex home id is required")
	}
	binary := strings.TrimSpace(os.Getenv("AGENT_BEACON_CODEX_BIN"))
	if binary == "" {
		var err error
		binary, err = exec.LookPath("codex")
		if err != nil {
			return AdapterOutput{}, errors.New("locate Codex CLI: set AGENT_BEACON_CODEX_BIN or install codex in PATH")
		}
	}
	command := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		return AdapterOutput{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return AdapterOutput{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return AdapterOutput{}, err
	}
	if err := command.Start(); err != nil {
		return AdapterOutput{}, fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()
	stderrData := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(stderr, 4096))
		stderrData <- strings.TrimSpace(string(data))
	}()

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{
		"method": "initialize", "id": 0,
		"params": map[string]any{"clientInfo": map[string]string{
			"name": "agent_beacon", "title": "Agent Beacon", "version": "0.1.0",
		}},
	}); err != nil {
		return AdapterOutput{}, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxAppServerLineBytes)
	initialize, err := readAppServerResponse(scanner, 0)
	if err != nil {
		return AdapterOutput{}, err
	}
	if initialize.Error != nil {
		return AdapterOutput{}, fmt.Errorf("Codex app-server initialize: %s", initialize.Error.Message)
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return AdapterOutput{}, err
	}
	if err := encoder.Encode(map[string]any{"method": "account/rateLimits/read", "id": 1, "params": nil}); err != nil {
		return AdapterOutput{}, err
	}
	message, err := readAppServerResponse(scanner, 1)
	if err != nil {
		return AdapterOutput{}, err
	}
	if message.Error != nil {
		return AdapterOutput{}, fmt.Errorf("Codex account/rateLimits/read: %s", message.Error.Message)
	}
	var response appServerRateResponse
	if err := json.Unmarshal(message.Result, &response); err != nil {
		return AdapterOutput{}, fmt.Errorf("decode Codex rate limits: %w", err)
	}
	limits := response.RateLimits
	if current, ok := response.RateLimitsByLimitID["codex"]; ok {
		limits = current
	}
	weekly, err := selectWeeklyWindow(limits)
	if err != nil {
		return AdapterOutput{}, err
	}
	output := AdapterOutput{SchemaVersion: 1, HomeID: homeID, ObservedAt: time.Now().UTC()}
	output.Weekly.RemainingPercent = 100 - weekly.UsedPercent
	if output.Weekly.RemainingPercent < 0 {
		output.Weekly.RemainingPercent = 0
	}
	if output.Weekly.RemainingPercent > 100 {
		output.Weekly.RemainingPercent = 100
	}
	if weekly.ResetsAt != nil {
		value := time.Unix(*weekly.ResetsAt, 0).UTC()
		output.Weekly.ResetAt = &value
	}
	if response.RateLimitResetCredits != nil {
		available := response.RateLimitResetCredits.AvailableCount
		output.ResetCards.Available = &available
		for _, credit := range response.RateLimitResetCredits.Credits {
			if credit.Status != "available" || credit.ExpiresAt == nil {
				continue
			}
			expires := time.Unix(*credit.ExpiresAt, 0).UTC()
			if output.ResetCards.NearestExpiresAt == nil || expires.Before(*output.ResetCards.NearestExpiresAt) {
				output.ResetCards.NearestExpiresAt = &expires
			}
		}
	}
	return output, nil
}

func WriteAppServerQuota(ctx context.Context, homeID string, writer io.Writer) error {
	output, err := ReadAppServerQuota(ctx, homeID)
	if err != nil {
		return err
	}
	return json.NewEncoder(writer).Encode(output)
}

func readAppServerResponse(scanner *bufio.Scanner, id int) (appServerMessage, error) {
	for scanner.Scan() {
		var message appServerMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != nil && *message.ID == id {
			return message, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return appServerMessage{}, fmt.Errorf("read Codex app-server: %w", err)
	}
	return appServerMessage{}, errors.New("Codex app-server closed before returning rate limits")
}

func selectWeeklyWindow(limits appServerRateLimits) (*appServerWindow, error) {
	var selected *appServerWindow
	for _, window := range []*appServerWindow{limits.Primary, limits.Secondary} {
		if window == nil || window.WindowDurationMins == nil || *window.WindowDurationMins < 24*60 {
			continue
		}
		if selected == nil || *window.WindowDurationMins > *selected.WindowDurationMins {
			selected = window
		}
	}
	if selected == nil {
		return nil, errors.New("Codex app-server did not return a weekly rate-limit window")
	}
	return selected, nil
}
