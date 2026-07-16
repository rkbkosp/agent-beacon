package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"agent-beacon/internal/protocol"
)

const (
	tokenRateMetric       = "completion_output_tokens_per_second"
	maxTokenRateStateSize = 64 * 1024
	maxTokenRateWindowMS  = 10 * 60 * 1000
	maxTokensPerSecond    = 10000
)

type tokenRateStateFile struct {
	Version            int     `json:"version"`
	Metric             string  `json:"metric"`
	Estimated          bool    `json:"estimated"`
	TokensPerSecond    float64 `json:"tokens_per_second"`
	RawTokensPerSecond float64 `json:"raw_tokens_per_second"`
	ActiveSessions     int     `json:"active_sessions"`
	ActiveStreams      int     `json:"active_streams"`
	ToolActiveStreams  *int    `json:"tool_active_streams"`
	WindowMS           uint64  `json:"window_ms"`
	UpdatedAtUnixMS    uint64  `json:"updated_at_unix_ms"`
}

func readTokenRateState(path string, now time.Time, staleAfter time.Duration) (protocol.TokenRateState, error) {
	metadata, err := os.Lstat(path)
	if err != nil {
		return protocol.TokenRateState{}, fmt.Errorf("inspect token-rate state: %w", err)
	}
	if !metadata.Mode().IsRegular() {
		return protocol.TokenRateState{}, errors.New("token-rate state must be a regular file")
	}
	if metadata.Size() > maxTokenRateStateSize {
		return protocol.TokenRateState{}, errors.New("token-rate state exceeds 65536 bytes")
	}
	if metadata.Mode().Perm()&0o177 != 0 || metadata.Mode().Perm()&0o400 == 0 {
		return protocol.TokenRateState{}, fmt.Errorf("token-rate state permissions must not exceed 0600: got %04o", metadata.Mode().Perm())
	}

	file, err := os.Open(path)
	if err != nil {
		return protocol.TokenRateState{}, fmt.Errorf("open token-rate state: %w", err)
	}
	defer file.Close()
	openedMetadata, err := file.Stat()
	if err != nil {
		return protocol.TokenRateState{}, fmt.Errorf("stat token-rate state: %w", err)
	}
	if !os.SameFile(metadata, openedMetadata) {
		return protocol.TokenRateState{}, errors.New("token-rate state changed while opening")
	}

	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(file, maxTokenRateStateSize+1)))
	decoder.DisallowUnknownFields()
	var stateFile tokenRateStateFile
	if err := decoder.Decode(&stateFile); err != nil {
		return protocol.TokenRateState{}, fmt.Errorf("decode token-rate state: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return protocol.TokenRateState{}, err
	}
	if stateFile.Version != 1 || stateFile.Metric != tokenRateMetric || !stateFile.Estimated {
		return protocol.TokenRateState{}, errors.New("token-rate state has an unsupported contract")
	}
	if invalidRate(stateFile.TokensPerSecond) || invalidRate(stateFile.RawTokensPerSecond) {
		return protocol.TokenRateState{}, errors.New("token-rate state contains an invalid rate")
	}
	if stateFile.ToolActiveStreams == nil || stateFile.ActiveSessions < 0 || stateFile.ActiveStreams < 0 ||
		*stateFile.ToolActiveStreams < 0 || stateFile.ActiveSessions > stateFile.ActiveStreams ||
		*stateFile.ToolActiveStreams > stateFile.ActiveStreams {
		return protocol.TokenRateState{}, errors.New("token-rate state contains invalid activity counts")
	}
	if stateFile.WindowMS == 0 || stateFile.WindowMS > maxTokenRateWindowMS ||
		stateFile.UpdatedAtUnixMS == 0 || stateFile.UpdatedAtUnixMS > math.MaxInt64 {
		return protocol.TokenRateState{}, errors.New("token-rate state contains invalid timing")
	}

	updatedAt := time.UnixMilli(int64(stateFile.UpdatedAtUnixMS))
	if updatedAt.After(now.Add(5 * time.Second)) {
		return protocol.TokenRateState{}, errors.New("token-rate state timestamp is in the future")
	}
	value := stateFile.TokensPerSecond
	result := protocol.TokenRateState{
		TokensPerSecond: &value,
		ActiveSessions:  stateFile.ActiveSessions,
		ActiveStreams:   stateFile.ActiveStreams,
		WindowMS:        uint32(stateFile.WindowMS),
		Estimated:       true,
		UpdatedAt:       &updatedAt,
		Freshness:       protocol.FreshnessFresh,
	}
	if now.Sub(updatedAt) > staleAfter {
		result.TokensPerSecond = nil
		result.ActiveSessions = 0
		result.ActiveStreams = 0
		result.Freshness = protocol.FreshnessStale
	}
	return result, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode token-rate state trailer: %w", err)
	}
	return errors.New("decode token-rate state: multiple JSON values")
}

func invalidRate(value float64) bool {
	return value < 0 || value > maxTokensPerSecond || math.IsNaN(value) || math.IsInf(value, 0)
}
