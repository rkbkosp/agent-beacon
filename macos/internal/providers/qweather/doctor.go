package qweather

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"agent-beacon/internal/config"
)

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type doctorDependencies struct {
	now     func() time.Time
	resolve func(context.Context, string) ([]string, error)
	tls     func(context.Context, string) error
}

type DoctorOption func(*doctorDependencies)

func WithDoctorClock(now func() time.Time) DoctorOption {
	return func(dependencies *doctorDependencies) { dependencies.now = now }
}

func WithDoctorResolver(resolve func(context.Context, string) ([]string, error)) DoctorOption {
	return func(dependencies *doctorDependencies) { dependencies.resolve = resolve }
}

func WithDoctorTLS(check func(context.Context, string) error) DoctorOption {
	return func(dependencies *doctorDependencies) { dependencies.tls = check }
}

func Doctor(ctx context.Context, weather config.WeatherConfig, client *Client, options ...DoctorOption) []Check {
	dependencies := doctorDependencies{
		now: time.Now,
		resolve: func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		},
		tls: defaultTLSCheck,
	}
	for _, option := range options {
		option(&dependencies)
	}
	doctorNow := dependencies.now()
	checks := make([]Check, 0, 16)
	add := func(name string, err error, success string) {
		if err != nil {
			checks = append(checks, Check{Name: name, Detail: err.Error()})
			return
		}
		checks = append(checks, Check{Name: name, OK: true, Detail: success})
	}

	add("configuration", config.ValidateWeather(weather), "weather configuration is valid")
	info, statErr := os.Stat(weather.PrivateKeyPath)
	add("private_key_exists", statErr, "private key file is readable")
	permissionErr := statErr
	if permissionErr == nil && info.Mode().Perm()&^os.FileMode(0o600) != 0 {
		permissionErr = fmt.Errorf("private key permissions are %04o; run chmod 600 on the configured file", info.Mode().Perm())
	}
	add("private_key_permissions", permissionErr, "private key permissions are no wider than 0600")

	signer, signerErr := LoadJWTSigner(weather.PrivateKeyPath, weather.CredentialID, weather.ProjectID)
	add("private_key_format", signerErr, "private key is PKCS#8 Ed25519")
	var token string
	if signerErr == nil {
		token, signerErr = signer.Token(doctorNow)
	}
	add("jwt_generation", signerErr, "dynamic Ed25519 JWT generated in memory")
	if signerErr == nil {
		header, payload, decodeErr := decodeDoctorJWT(token)
		headerErr := decodeErr
		if headerErr == nil && (header.Algorithm != "EdDSA" || header.CredentialID != weather.CredentialID) {
			headerErr = fmt.Errorf("JWT Header alg/kid does not match weather configuration")
		}
		add("jwt_header", headerErr, "JWT Header alg=EdDSA and kid matches credential_id")
		payloadErr := decodeErr
		now := doctorNow.Unix()
		if payloadErr == nil && (payload.ProjectID != weather.ProjectID || payload.IssuedAt != now-30 || payload.ExpiresAt != doctorNow.Add(15*time.Minute).Unix()) {
			payloadErr = fmt.Errorf("JWT Payload sub/iat/exp does not match weather configuration")
		}
		add("jwt_payload", payloadErr, "JWT Payload sub/iat/exp are valid")
	} else {
		add("jwt_header", fmt.Errorf("unavailable because JWT generation failed"), "")
		add("jwt_payload", fmt.Errorf("unavailable because JWT generation failed"), "")
	}

	clockErr := error(nil)
	if year := doctorNow.UTC().Year(); year < 2020 || year > 2100 {
		clockErr = fmt.Errorf("system clock year %d is unreasonable; synchronize macOS time", year)
	}
	add("system_time", clockErr, "system time is within a reasonable range")
	addresses, resolveErr := dependencies.resolve(ctx, weather.APIHost)
	if resolveErr == nil && len(addresses) == 0 {
		resolveErr = fmt.Errorf("DNS returned no addresses")
	}
	add("dns", resolveErr, "account-specific API Host resolves")
	add("https", dependencies.tls(ctx, weather.APIHost), "TLS connection to API Host succeeds")

	if client == nil {
		add("weather_now", fmt.Errorf("qweather client is unavailable"), "")
		add("weather_24h", fmt.Errorf("qweather client is unavailable"), "")
		add("weather_72h", fmt.Errorf("qweather client is unavailable"), "")
		add("forecast_targets", fmt.Errorf("qweather client is unavailable"), "")
		add("attribution", fmt.Errorf("qweather client is unavailable"), "")
		return checks
	}
	nowData, nowErr := client.FetchNow(ctx)
	add("weather_now", nowErr, "QWeather now endpoint succeeds")
	hourly24, hourlyErr := client.FetchHourly(ctx, 24*time.Hour)
	add("weather_24h", hourlyErr, "QWeather 24-hour endpoint succeeds")
	targets, targetsErr := TargetsFor(doctorNow, weather.Timezone, weather.Schedule)
	var selectedHourly HourlyData
	if hourlyErr == nil {
		selectedHourly = hourly24
	}
	if targetsErr == nil && RequiredHorizon(doctorNow, targets) > 24*time.Hour {
		hourly72, hourly72Err := client.FetchHourly(ctx, RequiredHorizon(doctorNow, targets))
		add("weather_72h", hourly72Err, "QWeather 72-hour endpoint succeeds for the next active day")
		if hourly72Err == nil {
			selectedHourly = hourly72
		}
	} else {
		add("weather_72h", targetsErr, "72-hour endpoint is not required for current targets")
	}
	if targetsErr == nil && len(selectedHourly.Points) > 0 {
		localNow := doctorNow.In(targets.Lunch.Location())
		for _, target := range []struct {
			name string
			at   time.Time
		}{{name: "lunch", at: targets.Lunch}, {name: "leave", at: targets.Leave}} {
			// QWeather's hourly endpoint only returns forecast hours. A target that
			// has already passed can only be retained from our last-good cache and
			// therefore must not make a live connectivity diagnostic fail.
			if !localNow.Before(target.at) {
				continue
			}
			if _, ok := SelectForecast(selectedHourly.Points, target.at); !ok {
				targetsErr = fmt.Errorf("hourly response does not cover future %s target %s", target.name, target.at.Format(time.RFC3339))
				break
			}
		}
	} else if targetsErr == nil {
		targetsErr = fmt.Errorf("hourly response is unavailable")
	}
	add("forecast_targets", targetsErr, "configured future lunch and leave forecast records are available; past targets use last-good cache")
	attributionErr := error(nil)
	if nowErr != nil || hourlyErr != nil || len(nowData.Refer.Sources) == 0 || len(nowData.Refer.License) == 0 ||
		len(hourly24.Refer.Sources) == 0 || len(hourly24.Refer.License) == 0 {
		attributionErr = fmt.Errorf("QWeather refer.sources and refer.license must be present in successful responses")
	}
	add("attribution", attributionErr, "QWeather source and license metadata are retained")
	if weather.PublicKeyPath != "" {
		add("public_key_pair", verifyPublicKey(weather.PublicKeyPath, signer), "optional public key matches the configured private key")
	}
	return checks
}

func FormatChecks(checks []Check) string {
	var output strings.Builder
	for _, check := range checks {
		status := "PASS"
		if !check.OK {
			status = "FAIL"
		}
		fmt.Fprintf(&output, "[%s] %s: %s\n", status, check.Name, check.Detail)
	}
	return output.String()
}

func formatChecks(checks []Check) string { return FormatChecks(checks) }

func ChecksOK(checks []Check) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func decodeDoctorJWT(token string) (jwtHeader, jwtPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtHeader{}, jwtPayload{}, fmt.Errorf("generated JWT does not have three segments")
	}
	var header jwtHeader
	var payload jwtPayload
	for index, target := range []any{&header, &payload} {
		data, err := base64.RawURLEncoding.DecodeString(parts[index])
		if err != nil {
			return jwtHeader{}, jwtPayload{}, fmt.Errorf("decode generated JWT metadata: %w", err)
		}
		if err := json.Unmarshal(data, target); err != nil {
			return jwtHeader{}, jwtPayload{}, fmt.Errorf("decode generated JWT metadata: %w", err)
		}
	}
	return header, payload, nil
}

func defaultTLSCheck(ctx context.Context, host string) error {
	dialer := &tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return fmt.Errorf("connect QWeather HTTPS: %w", err)
	}
	return connection.Close()
}

func verifyPublicKey(path string, signer *JWTSigner) error {
	if signer == nil {
		return fmt.Errorf("private key is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read qweather public key: %w", err)
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		return fmt.Errorf("qweather public key must be a PEM PUBLIC KEY")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse qweather public key: %w", err)
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("qweather public key is not Ed25519")
	}
	want := signer.privateKey.Public().(ed25519.PublicKey)
	if !public.Equal(want) {
		return fmt.Errorf("qweather public key does not match the private key")
	}
	return nil
}
