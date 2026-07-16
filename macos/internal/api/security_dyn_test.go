package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-beacon/internal/protocol"
	"agent-beacon/internal/state"
	"github.com/gorilla/websocket"
)

// Dynamic penetration probes against a live httptest Bridge.
// These are adversarial HTTP/WS checks, not unit coverage of happy paths.

func TestDynamicAuthBypassMatrix(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 32), DefaultSnapshot(), testToken)
	bridge.SetFixturesEnabled(false)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	protected := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/readyz"},
		{http.MethodGet, "/v2/snapshot"},
		{http.MethodGet, "/v2/events"},
		{http.MethodGet, "/v2/devices"},
		{http.MethodPost, "/v2/notifications"},
		{http.MethodPost, "/v2/fixtures/herdr-blocked"},
	}

	tokenVariants := []string{
		"",
		" ",
		testToken + "x",
		testToken[:len(testToken)-1],
		strings.ToUpper(testToken),
		"Bearer " + testToken,
		testToken[:1],
		testToken + "x" + testToken,
		"null",
		"undefined",
		"0",
	}

	for _, endpoint := range protected {
		for _, token := range tokenVariants {
			name := fmt.Sprintf("%s %s token=%q", endpoint.method, endpoint.path, token)
			t.Run(name, func(t *testing.T) {
				var body io.Reader
				if endpoint.method == http.MethodPost {
					body = strings.NewReader(`{}`)
				}
				request, err := http.NewRequest(endpoint.method, server.URL+endpoint.path, body)
				if err != nil {
					t.Fatal(err)
				}
				if token != "" {
					request.Header.Set("X-Agent-Beacon-Token", token)
				}
				if endpoint.method == http.MethodPost {
					request.Header.Set("Content-Type", "application/json")
				}
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusUnauthorized {
					payload, _ := io.ReadAll(io.LimitReader(response.Body, 512))
					t.Fatalf("expected 401, got %d body=%s", response.StatusCode, payload)
				}
			})
		}
	}

	// healthz must stay public
	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", response.StatusCode)
	}
}

func TestDynamicNotificationInjectionAndLimits(t *testing.T) {
	bridge := NewServerWithLimits(state.NewStore(time.Minute, 32), DefaultSnapshot(), testToken, 8, 1024)
	bridge.SetFixturesEnabled(false)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	post := func(raw []byte, contentType string) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v2/notifications", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Agent-Beacon-Token", testToken)
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	t.Run("wrong content type", func(t *testing.T) {
		response := post([]byte(`{}`), "text/plain")
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("oversized body", func(t *testing.T) {
		response := post(bytes.Repeat([]byte("a"), 2048), "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		response := post([]byte(`{"v":2`), "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("wrong envelope type", func(t *testing.T) {
		envelope, err := protocol.NewEnvelope("hb-1", protocol.TypeHeartbeat, 0, time.Now().UTC(), protocol.Heartbeat{})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(envelope)
		response := post(data, "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("nonzero revision rejected", func(t *testing.T) {
		envelope := notificationEnvelope(t, "rev-1", "system:dyn:rev", time.Now().Add(time.Minute))
		envelope.Revision = 9
		data, _ := json.Marshal(envelope)
		response := post(data, "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("expired notification", func(t *testing.T) {
		envelope := notificationEnvelope(t, "exp-1", "system:dyn:exp", time.Now().Add(-time.Second))
		data, _ := json.Marshal(envelope)
		response := post(data, "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusGone {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})

	t.Run("valid then duplicate", func(t *testing.T) {
		envelope := notificationEnvelope(t, "ok-1", "system:dyn:dedupe", time.Now().Add(time.Minute))
		data, _ := json.Marshal(envelope)
		first := post(data, "application/json")
		defer first.Body.Close()
		if first.StatusCode != http.StatusAccepted {
			t.Fatalf("first status = %d", first.StatusCode)
		}
		second := post(data, "application/json")
		defer second.Body.Close()
		if second.StatusCode != http.StatusOK {
			t.Fatalf("duplicate status = %d", second.StatusCode)
		}
	})

	t.Run("oversized title rejected by protocol", func(t *testing.T) {
		_, err := protocol.NewEnvelope("xss-1", protocol.TypeNotification, 0, time.Now().UTC(), protocol.Notification{
			Category: protocol.CategorySystem, Kind: "system.provider_error", Source: "dyn",
			SubjectID: "s", Theme: protocol.ThemeRed, Urgency: protocol.UrgencyUrgent,
			Priority: 90, DedupeKey: "system:dyn:xss",
			Title: strings.Repeat("超", 29), Detail: "x", SourceLabel: "Dyn",
			DisplayMS: 3000, ExpiresAt: time.Now().Add(time.Minute),
		})
		if err == nil {
			t.Fatal("expected protocol to reject oversized title")
		}
	})

	t.Run("html title accepted as plain text not executed", func(t *testing.T) {
		// LCD path has no HTML renderer; protocol only bounds length.
		envelope, err := protocol.NewEnvelope("html-1", protocol.TypeNotification, 0, time.Now().UTC(), protocol.Notification{
			Category: protocol.CategorySystem, Kind: "system.provider_error", Source: "dyn",
			SubjectID: "s", Theme: protocol.ThemeYellow, Urgency: protocol.UrgencyNormal,
			Priority: 40, DedupeKey: "system:dyn:html",
			Title: "<b>hi</b>", Detail: "plain", SourceLabel: "Dyn",
			DisplayMS: 3000, ExpiresAt: time.Now().Add(time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(envelope)
		response := post(data, "application/json")
		defer response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})
}

func TestDynamicFixturesDisabledWhenMockOff(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 16), DefaultSnapshot(), testToken)
	bridge.SetFixturesEnabled(false)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v2/fixtures/herdr-blocked", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Agent-Beacon-Token", testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestDynamicWebSocketAuthAndImpersonation(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 16), DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	dial := func(headers http.Header) (*websocket.Conn, *http.Response, error) {
		url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/ws"
		return websocket.DefaultDialer.Dial(url, headers)
	}

	t.Run("missing credentials", func(t *testing.T) {
		conn, response, err := dial(nil)
		if err == nil {
			conn.Close()
			t.Fatal("expected dial failure")
		}
		if response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("response = %#v err = %v", response, err)
		}
	})

	t.Run("wrong protocol version", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Agent-Beacon-Device-ID", "dev-a")
		headers.Set("X-Agent-Beacon-Token", testToken)
		headers.Set("X-Agent-Beacon-Protocol", "1")
		conn, response, err := dial(headers)
		if err == nil {
			conn.Close()
			t.Fatal("expected dial failure")
		}
		if response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("response = %#v err = %v", response, err)
		}
	})

	t.Run("valid connect receives hello", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Agent-Beacon-Device-ID", "dev-valid")
		headers.Set("X-Agent-Beacon-Token", testToken)
		headers.Set("X-Agent-Beacon-Protocol", "2")
		conn, _, err := dial(headers)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := protocol.Decode(data)
		if err != nil || envelope.Type != protocol.TypeHello {
			t.Fatalf("first message = %s err = %v", data, err)
		}
	})

	// Documented residual risk: any token holder can claim arbitrary device IDs.
	t.Run("same token can open many device ids", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			headers := http.Header{}
			headers.Set("X-Agent-Beacon-Device-ID", fmt.Sprintf("spoof-%d", i))
			headers.Set("X-Agent-Beacon-Token", testToken)
			headers.Set("X-Agent-Beacon-Protocol", "2")
			conn, _, err := dial(headers)
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
		}
		request := authorizedRequest(t, http.MethodGet, server.URL+"/v2/devices")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})
}

func TestDynamicUSBHelloWithoutTokenRejected(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 8), DefaultSnapshot(), testToken)
	transport := newChannelDeviceTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = bridge.ServeDeviceTransport(ctx, transport) }()

	// Drain server hello.
	select {
	case <-transport.writes:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server hello")
	}

	badHello, err := protocol.NewEnvelope("hello-bad", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "usb-dev", ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(badHello)
	transport.reads <- data

	// Device must not become ready / receive snapshot.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case msg := <-transport.writes:
			envelope, decodeErr := protocol.Decode(msg)
			if decodeErr == nil && envelope.Type == protocol.TypeSnapshot {
				t.Fatal("unauthenticated USB hello received snapshot")
			}
		case <-deadline:
			return
		}
	}
}

func TestDynamicSlowDeviceDoesNotBlockBroadcast(t *testing.T) {
	bridge := NewServerWithLimits(state.NewStore(time.Minute, 32), DefaultSnapshot(), testToken, 1, defaultMaxRequestBytes)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	// Slow WS client: never read after connect.
	headers := http.Header{}
	headers.Set("X-Agent-Beacon-Device-ID", "slow")
	headers.Set("X-Agent-Beacon-Token", testToken)
	headers.Set("X-Agent-Beacon-Protocol", "2")
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v2/ws"
	slow, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()

	// Complete handshake so client is ready (read hello + send device hello).
	_ = slow.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = slow.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	hello, err := protocol.NewEnvelope("hello-slow", protocol.TypeHello, 0, time.Now().UTC(), protocol.Hello{
		Role: "device", DeviceID: "slow", ProtocolVersion: protocol.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	helloBytes, _ := json.Marshal(hello)
	if err := slow.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		t.Fatal(err)
	}
	// Read snapshot to mark ready path complete, then stop reading to fill queue.
	_ = slow.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, _ = slow.ReadMessage()

	// Fast client should still accept notifications while slow fills/disconnects.
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			envelope := notificationEnvelope(t, fmt.Sprintf("flood-%d", index),
				fmt.Sprintf("system:dyn:flood:%d", index), time.Now().Add(time.Minute))
			data, _ := json.Marshal(envelope)
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v2/notifications", bytes.NewReader(data))
			if err != nil {
				return
			}
			request.Header.Set("X-Agent-Beacon-Token", testToken)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return
			}
			defer response.Body.Close()
			if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusOK {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() == 0 {
		t.Fatal("broadcast path stalled under slow device pressure")
	}
}

func TestDynamicConcurrentAuthHammer(t *testing.T) {
	bridge := NewServer(state.NewStore(time.Minute, 16), DefaultSnapshot(), testToken)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	var unauthorized atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			request, err := http.NewRequest(http.MethodGet, server.URL+"/v2/snapshot", nil)
			if err != nil {
				return
			}
			request.Header.Set("X-Agent-Beacon-Token", fmt.Sprintf("guess-%d", index))
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				return
			}
			defer response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				unauthorized.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if unauthorized.Load() != 64 {
		t.Fatalf("unauthorized count = %d", unauthorized.Load())
	}
}
