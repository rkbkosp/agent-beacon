package qweather

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxCacheBytes = 4 << 20

type CacheRecord struct {
	Provider    string          `json:"provider"`
	Endpoint    string          `json:"endpoint"`
	Location    string          `json:"location"`
	FetchedAt   time.Time       `json:"fetched_at"`
	UpdateTime  string          `json:"update_time"`
	PayloadJSON json.RawMessage `json:"payload_json"`
}

type CacheStore interface {
	Load() ([]CacheRecord, error)
	Save([]CacheRecord) error
	Clear() error
}

type FileCache struct {
	path string
	mu   sync.Mutex
}

type cacheFile struct {
	Version int           `json:"version"`
	Records []CacheRecord `json:"records"`
}

func NewFileCache(path string) *FileCache { return &FileCache{path: path} }

func DefaultCachePath() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(directory, "AgentBeacon", "qweather-cache.json"), nil
}

func (cache *FileCache) Path() string { return cache.path }

func (cache *FileCache) Load() ([]CacheRecord, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	data, err := os.ReadFile(cache.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read qweather cache: %w", err)
	}
	if len(data) > maxCacheBytes {
		return nil, errors.New("qweather cache exceeds 4 MiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value cacheFile
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode qweather cache: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode qweather cache: trailing JSON data")
	}
	if value.Version != 1 {
		return nil, fmt.Errorf("unsupported qweather cache version %d", value.Version)
	}
	for _, record := range value.Records {
		if err := validateCacheRecord(record); err != nil {
			return nil, err
		}
	}
	return append([]CacheRecord(nil), value.Records...), nil
}

func (cache *FileCache) Save(records []CacheRecord) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, record := range records {
		if err := validateCacheRecord(record); err != nil {
			return err
		}
	}
	directory := filepath.Dir(cache.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create qweather cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".qweather-cache-*")
	if err != nil {
		return fmt.Errorf("create qweather cache temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure qweather cache temporary file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	if err := encoder.Encode(cacheFile{Version: 1, Records: records}); err != nil {
		return fmt.Errorf("encode qweather cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync qweather cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close qweather cache: %w", err)
	}
	if err := os.Rename(temporaryPath, cache.path); err != nil {
		return fmt.Errorf("replace qweather cache: %w", err)
	}
	if err := os.Chmod(cache.path, 0o600); err != nil {
		return fmt.Errorf("secure qweather cache: %w", err)
	}
	keep = true
	return nil
}

func (cache *FileCache) Clear() error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := os.Remove(cache.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear qweather cache: %w", err)
	}
	return nil
}

func validateCacheRecord(record CacheRecord) error {
	if record.Provider != "qweather" {
		return errors.New("qweather cache record provider must be qweather")
	}
	switch record.Endpoint {
	case "/v7/weather/now", "/v7/weather/24h", "/v7/weather/72h":
	default:
		return fmt.Errorf("qweather cache record endpoint %q is invalid", record.Endpoint)
	}
	if record.Location == "" || record.FetchedAt.IsZero() || !json.Valid(record.PayloadJSON) {
		return errors.New("qweather cache record location, fetched_at, and valid payload_json are required")
	}
	return nil
}
