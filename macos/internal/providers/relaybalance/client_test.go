package relaybalance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type rewriteTransport struct {
	target http.RoundTripper
	url    string
}

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	target, _ := http.NewRequest(http.MethodGet, transport.url, nil)
	copy.URL = target.URL
	return transport.target.RoundTrip(copy)
}

func TestFetchUsesKeychainValueAndRemaining(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-secret" || request.Header.Get("Accept") != "application/json" {
			t.Fatal("relay request headers are invalid")
		}
		_, _ = writer.Write([]byte(`{"balance":99,"remaining":14.16,"isValid":true,"unit":"USD"}`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: "https://api.0-0.pro/v1/usage", SecretName: "zero-api-key", Timeout: time.Second},
		&http.Client{Transport: rewriteTransport{target: http.DefaultTransport, url: server.URL}},
		func(context.Context, string) (string, error) { return "test-secret", nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Fetch(context.Background())
	if err != nil || result.Remaining != 14.16 || result.Unit != "USD" || !result.IsValid {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestFetchFallsBackToBalanceAndRejectsInvalidCredential(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want float64
		err  error
	}{
		{name: "balance", body: `{"balance":8.5,"isValid":true,"unit":"USD"}`, want: 8.5},
		{name: "invalid", body: `{"balance":8.5,"isValid":false,"unit":"USD"}`, err: ErrInvalidCredentials},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(test.body)) }))
			defer server.Close()
			client, _ := New(Config{Endpoint: "https://api.0-0.pro/v1/usage", SecretName: "key", Timeout: time.Second},
				&http.Client{Transport: rewriteTransport{target: http.DefaultTransport, url: server.URL}},
				func(context.Context, string) (string, error) { return "secret", nil })
			result, err := client.Fetch(context.Background())
			if !errors.Is(err, test.err) || result.Remaining != test.want {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
		})
	}
}

func TestErrorsNeverContainSecret(t *testing.T) {
	client, _ := New(Config{Endpoint: "https://api.0-0.pro/v1/usage", SecretName: "key", Timeout: time.Second},
		&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })},
		func(context.Context, string) (string, error) { return "never-print-me", nil })
	_, err := client.Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), "never-print-me") {
		t.Fatalf("unsafe error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
