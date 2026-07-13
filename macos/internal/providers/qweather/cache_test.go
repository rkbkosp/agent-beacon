package qweather

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileCacheRoundTripsOnlyLastGoodWeatherPayloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "qweather-cache.json")
	cache := NewFileCache(path)
	records := []CacheRecord{
		{Provider: "qweather", Endpoint: "/v7/weather/now", Location: "101210101", FetchedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), UpdateTime: "2026-07-14T20:00+08:00", PayloadJSON: json.RawMessage(`{"code":"200","now":{"temp":"29"}}`)},
		{Provider: "qweather", Endpoint: "/v7/weather/24h", Location: "101210101", FetchedAt: time.Date(2026, 7, 14, 12, 1, 0, 0, time.UTC), UpdateTime: "2026-07-14T20:00+08:00", PayloadJSON: json.RawMessage(`{"code":"200","hourly":[]}`)},
	}
	if err := cache.Save(records); err != nil {
		t.Fatal(err)
	}
	got, err := cache.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(records) || got[0].Endpoint != records[0].Endpoint || !bytes.Equal(got[1].PayloadJSON, records[1].PayloadJSON) {
		t.Fatalf("cache records = %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secretName := range []string{"private_key", "credential_id", "project_id", "authorization", "jwt"} {
		if strings.Contains(strings.ToLower(string(raw)), secretName) {
			t.Fatalf("cache contains secret field %q", secretName)
		}
	}
}

func TestFileCacheWriteIsAtomicAndPermissionsAre0600(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "qweather-cache.json")
	cache := NewFileCache(path)
	if err := cache.Save([]CacheRecord{{Provider: "qweather", Endpoint: "/v7/weather/now", Location: "101210101", FetchedAt: time.Now(), PayloadJSON: json.RawMessage(`{"code":"200"}`)}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("cache directory entries = %v", entries)
	}
}

func TestFileCacheClearIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qweather-cache.json")
	cache := NewFileCache(path)
	if err := cache.Save([]CacheRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache still exists: %v", err)
	}
}
