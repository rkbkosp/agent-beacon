package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agent-beacon/internal/api"
	"agent-beacon/internal/config"
	"agent-beacon/internal/protocol"
	"agent-beacon/internal/providers"
	"agent-beacon/internal/providers/codex"
	"agent-beacon/internal/providers/herdr"
	"agent-beacon/internal/providers/qweather"
	"agent-beacon/internal/providers/relaybalance"
	"agent-beacon/internal/secrets"
	"agent-beacon/internal/state"
	"agent-beacon/internal/usbtransport"
)

const defaultServer = "http://127.0.0.1:8787"

var weatherHTTPClientFactory = func(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: agent-beacon-bridge <serve|doctor|status|snapshot|devices|emit|weather|secret|install-service|uninstall-service>")
		return 2
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "status":
		return runGet(ctx, "status", "/v2/events", args[1:], stdout, stderr)
	case "snapshot":
		return runGet(ctx, "snapshot", "/v2/snapshot", args[1:], stdout, stderr)
	case "devices":
		return runGet(ctx, "devices", "/v2/devices", args[1:], stdout, stderr)
	case "emit":
		return runEmit(ctx, args[1:], stdout, stderr)
	case "weather":
		return runWeather(ctx, args[1:], stdout, stderr)
	case "secret":
		return runSecret(ctx, args[1:], stdinReader(), stdout, stderr)
	case "codex-adapter":
		return runCodexAdapter(ctx, args[1:], stdout, stderr)
	case "install-service":
		return runInstallService(ctx, args[1:], stdout, stderr)
	case "uninstall-service":
		return runUninstallService(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "configs/config.example.yaml", "YAML configuration path")
	disableUSB := flags.Bool("disable-usb", false, "disable USB device transport for this run")
	if flags.Parse(args) != nil {
		return 2
	}
	settings, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *disableUSB {
		settings.Transports.USB.Enabled = false
	}
	snapshot := runtimeSnapshot(settings)
	activeProviders := make([]providers.Provider, 0, 3)
	if settings.Providers.Codex.Enabled {
		var relayClient *relaybalance.Client
		if settings.Providers.RelayBalance.Enabled {
			relayClient, err = relaybalance.New(relaybalance.Config{
				Endpoint:   settings.Providers.RelayBalance.Endpoint,
				SecretName: settings.Providers.RelayBalance.SecretName,
				Timeout:    settings.Providers.RelayBalance.Timeout,
			}, &http.Client{Timeout: settings.Providers.RelayBalance.Timeout}, secrets.Get)
			if err != nil {
				fmt.Fprintf(stderr, "Relay provider disabled: %v\n", err)
			}
		}
		codexProvider := codex.New(settings.Providers.Codex, settings.Providers.RelayBalance, relayClient)
		initialContext, cancel := context.WithTimeout(ctx, settings.Providers.Codex.Adapter.Timeout+time.Second)
		patch, snapshotErr := codexProvider.Snapshot(initialContext)
		cancel()
		if patch.Codex != nil {
			snapshot.Codex = *patch.Codex
		}
		if snapshotErr != nil {
			fmt.Fprintf(stderr, "Codex/relay initial snapshot partially unavailable: %v\n", snapshotErr)
		}
		activeProviders = append(activeProviders, codexProvider)
	}
	if settings.Providers.Herdr.Enabled {
		herdrProvider := herdr.New(herdr.Config{
			Session: settings.Providers.Herdr.Session, SocketPath: settings.Providers.Herdr.SocketPath,
			ReconnectMax:       settings.Providers.Herdr.ReconnectMax,
			FullResyncInterval: settings.Providers.Herdr.FullResyncInterval,
		})
		initialContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		patch, snapshotErr := herdrProvider.Snapshot(initialContext)
		cancel()
		if snapshotErr != nil {
			fmt.Fprintf(stderr, "Herdr initial snapshot unavailable: %v\n", snapshotErr)
		} else if patch.Agents != nil {
			snapshot.Agents = *patch.Agents
		}
		activeProviders = append(activeProviders, herdrProvider)
	}
	if settings.Providers.Weather.ValidationError != "" {
		fmt.Fprintf(stderr, "Weather provider disabled: %s\n", settings.Providers.Weather.ValidationError)
	} else if settings.Providers.Weather.Enabled {
		weatherProvider, _, _, _, weatherErr := buildWeatherRuntime(settings.Providers.Weather)
		if weatherErr != nil {
			fmt.Fprintf(stderr, "Weather provider disabled: %v\n", weatherErr)
			if unknown, buildErr := qweather.BuildWeatherState(time.Now(), settings.Providers.Weather, nil, nil); buildErr == nil {
				snapshot.Weather = unknown
			}
		} else {
			initialContext, cancel := context.WithTimeout(ctx, settings.Providers.Weather.Refresh.RequestTimeout)
			patch, snapshotErr := weatherProvider.Snapshot(initialContext)
			cancel()
			if snapshotErr != nil {
				fmt.Fprintf(stderr, "Weather cached snapshot unavailable: %v\n", snapshotErr)
			} else if patch.Weather != nil {
				snapshot.Weather = *patch.Weather
			}
			activeProviders = append(activeProviders, weatherProvider)
		}
	}

	store := state.NewStore(settings.Notifications.DedupeWindow, settings.Notifications.QueueCapacity*8)
	bridge := api.NewServerWithLimits(store, snapshot, settings.Token,
		settings.Server.DeviceSendQueue, settings.Server.MaxRequestBytes)
	bridge.SetFixturesEnabled(settings.Providers.Mock.Enabled)
	httpServer := &http.Server{
		Addr: settings.Server.Listen, Handler: bridge.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go usbtransport.Run(ctx, usbtransport.Config{
		Enabled: settings.Transports.USB.Enabled, Port: settings.Transports.USB.Port,
		ScanInterval: settings.Transports.USB.ScanInterval,
	}, func(sessionContext context.Context, transport usbtransport.MessageTransport) error {
		return bridge.ServeDeviceTransport(sessionContext, transport)
	}, func(format string, arguments ...any) {
		fmt.Fprintf(stderr, format+"\n", arguments...)
	})
	updates := make(chan providers.Update, settings.Server.DeviceSendQueue)
	for _, currentProvider := range activeProviders {
		currentProvider := currentProvider
		go func() {
			providerErr := currentProvider.Start(ctx, updates)
			if providerErr != nil && !errors.Is(providerErr, context.Canceled) {
				fmt.Fprintf(stderr, "%s provider stopped: %v\n", currentProvider.Name(), providerErr)
			}
		}()
	}
	go func() {
		for {
			select {
			case update := <-updates:
				if publishErr := bridge.PublishProviderUpdate(update); publishErr != nil {
					fmt.Fprintf(stderr, "provider update rejected: %v\n", publishErr)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownContext)
	}()
	fmt.Fprintf(stdout, "Agent Beacon bridge listening on %s\n", settings.Server.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runtimeSnapshot(settings config.Config) protocol.Snapshot {
	now := time.Now()
	homes := make([]protocol.CodexHome, 0, len(settings.Providers.Codex.Homes))
	for _, home := range settings.Providers.Codex.Homes {
		homes = append(homes, protocol.CodexHome{
			ID: home.ID, Label: home.Label, UpdatedAt: now, Freshness: protocol.FreshnessUnknown,
		})
	}
	if len(homes) == 0 {
		homes = append(homes, protocol.CodexHome{ID: "codex", Label: "CODEX", UpdatedAt: now, Freshness: protocol.FreshnessUnknown})
	}
	timezone := settings.Providers.Weather.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	weather, err := qweather.BuildWeatherState(now, settings.Providers.Weather, nil, nil)
	if err != nil {
		location := settings.Providers.Weather.LocationLabel
		if location == "" {
			location = "未配置"
		}
		weather = protocol.WeatherState{
			Location: location, Provider: "qweather",
			Current:    protocol.WeatherCurrent{ObservedAt: now, Freshness: protocol.FreshnessUnknown},
			Lunch:      protocol.WeatherSlot{TargetAt: now, IsPast: true, Freshness: protocol.FreshnessUnknown},
			Leave:      protocol.WeatherSlot{TargetAt: now, IsPast: true, Freshness: protocol.FreshnessUnknown},
			NextOuting: protocol.NextOuting{Slot: "unknown", TargetAt: now, Confidence: "unknown", Reason: "天气配置不可用"},
			UpdatedAt:  now,
		}
	}
	return protocol.Snapshot{
		Clock: protocol.ClockState{Timezone: timezone, ServerTime: now},
		Codex: protocol.CodexState{
			Homes:     homes,
			Relay:     protocol.RelayState{Unit: "USD", UpdatedAt: now, Freshness: protocol.FreshnessUnknown},
			TokenRate: protocol.TokenRateState{Estimated: true, Freshness: protocol.FreshnessUnknown},
		},
		Agents:  protocol.AgentsState{Provider: "herdr", Connected: false, UpdatedAt: now, Items: []protocol.AgentItem{}},
		Weather: weather,
		System:  protocol.SystemState{BridgeOnline: true, OverallFreshness: protocol.FreshnessUnknown},
	}
}

func runCodexAdapter(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("codex-adapter", flag.ContinueOnError)
	flags.SetOutput(stderr)
	homeID := flags.String("home-id", os.Getenv("AGENT_BEACON_CODEX_HOME_ID"), "stable Codex home id")
	if flags.Parse(args) != nil {
		return 2
	}
	if err := codex.WriteAppServerQuota(ctx, *homeID, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runSecret(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "set" {
		fmt.Fprintln(stderr, "usage: agent-beacon-bridge secret set NAME (--from-env ENV|--stdin)")
		return 2
	}
	flags := flag.NewFlagSet("secret set", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fromEnv := flags.String("from-env", "", "read the secret from an environment variable")
	fromStdin := flags.Bool("stdin", false, "read the secret from stdin")
	if flags.Parse(args[2:]) != nil || (*fromEnv == "" && !*fromStdin) || (*fromEnv != "" && *fromStdin) {
		fmt.Fprintln(stderr, "secret set requires exactly one of --from-env or --stdin")
		return 2
	}
	var value string
	if *fromEnv != "" {
		value = os.Getenv(*fromEnv)
	} else {
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		value = string(data)
	}
	if err := secrets.Set(ctx, args[1], value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Stored %s in the Agent Beacon Keychain service\n", args[1])
	return 0
}

var stdinReader = func() io.Reader { return os.Stdin }

func runWeather(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: agent-beacon-bridge weather <doctor|fetch-now|fetch-hourly|fetch-radiation|snapshot|refresh|cache clear>")
		return 2
	}
	if args[0] == "cache" {
		if len(args) < 2 || args[1] != "clear" {
			fmt.Fprintln(stderr, "usage: agent-beacon-bridge weather cache clear [--config PATH]")
			return 2
		}
		flags := flag.NewFlagSet("weather cache clear", flag.ContinueOnError)
		flags.SetOutput(stderr)
		configPath := flags.String("config", "configs/config.example.yaml", "YAML configuration path")
		if flags.Parse(args[2:]) != nil {
			return 2
		}
		settings, err := config.LoadProviders(*configPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if settings.Providers.Weather.ValidationError != "" {
			fmt.Fprintln(stderr, settings.Providers.Weather.ValidationError)
			return 1
		}
		cachePath, err := qweather.DefaultCachePath()
		if err == nil {
			err = qweather.NewFileCache(cachePath).Clear()
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "QWeather cache cleared: %s\n", cachePath)
		return 0
	}

	flags := flag.NewFlagSet("weather "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "configs/config.example.yaml", "YAML configuration path")
	if flags.Parse(args[1:]) != nil {
		return 2
	}
	settings, err := config.LoadProviders(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	weather := settings.Providers.Weather
	if weather.ValidationError != "" {
		fmt.Fprintln(stderr, weather.ValidationError)
		return 1
	}
	if !weather.Enabled {
		fmt.Fprintln(stderr, "providers.weather.enabled must be true")
		return 1
	}
	provider, client, satelliteClient, _, err := buildWeatherRuntime(weather)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch args[0] {
	case "doctor":
		checks := qweather.Doctor(ctx, weather, client)
		fmt.Fprint(stdout, qweather.FormatChecks(checks))
		if !qweather.ChecksOK(checks) {
			return 1
		}
		return 0
	case "fetch-now":
		data, fetchErr := client.FetchNow(ctx)
		if fetchErr != nil {
			fmt.Fprintln(stderr, fetchErr)
			return 1
		}
		prettyJSON(stdout, data.Raw)
		return 0
	case "fetch-hourly":
		targets, targetErr := qweather.TargetsFor(time.Now(), weather.Timezone, weather.Schedule)
		if targetErr != nil {
			fmt.Fprintln(stderr, targetErr)
			return 1
		}
		data, fetchErr := client.FetchHourly(ctx, qweather.RequiredHorizon(time.Now(), targets))
		if fetchErr != nil {
			fmt.Fprintln(stderr, fetchErr)
			return 1
		}
		prettyJSON(stdout, data.Raw)
		return 0
	case "fetch-radiation":
		if satelliteClient == nil {
			fmt.Fprintln(stderr, "providers.weather.satellite_radiation.enabled must be true")
			return 1
		}
		data, fetchErr := satelliteClient.FetchRadiation(ctx)
		if fetchErr != nil {
			fmt.Fprintln(stderr, fetchErr)
			return 1
		}
		prettyJSON(stdout, data.Raw)
		return 0
	case "snapshot":
		patch, snapshotErr := provider.Snapshot(ctx)
		if snapshotErr != nil {
			fmt.Fprintln(stderr, snapshotErr)
			return 1
		}
		prettyValue(stdout, patch.Weather)
		return 0
	case "refresh":
		updates, refreshErr := provider.Refresh(ctx, true)
		for _, update := range updates {
			if update.Patch.Weather != nil {
				prettyValue(stdout, update.Patch.Weather)
				break
			}
		}
		if refreshErr != nil {
			fmt.Fprintln(stderr, refreshErr)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown weather command %q\n", args[0])
		return 2
	}
}

func buildWeatherRuntime(weather config.WeatherConfig) (*qweather.Provider, *qweather.Client, *qweather.SatelliteClient, *qweather.FileCache, error) {
	signer, err := qweather.LoadJWTSigner(weather.PrivateKeyPath, weather.CredentialID, weather.ProjectID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	client, err := qweather.NewClient(weather.APIHost, weather.Location, weather.Lang, signer,
		weatherHTTPClientFactory(weather.Refresh.RequestTimeout))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var satelliteClient *qweather.SatelliteClient
	options := make([]qweather.Option, 0, 1)
	if weather.Satellite.Enabled {
		satelliteClient, err = qweather.NewSatelliteClient(weather.Satellite.Latitude, weather.Satellite.Longitude,
			weather.Timezone, weatherHTTPClientFactory(weather.Refresh.RequestTimeout))
		if err != nil {
			return nil, nil, nil, nil, err
		}
		options = append(options, qweather.WithRadiationClient(satelliteClient))
	}
	cachePath, err := qweather.DefaultCachePath()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cache := qweather.NewFileCache(cachePath)
	provider, err := qweather.New(weather, client, cache, options...)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return provider, client, satelliteClient, cache, nil
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	server := flags.String("server", defaultServer, "bridge HTTP base URL")
	token := flags.String("token", os.Getenv("AGENT_BEACON_TOKEN"), "bridge token")
	if flags.Parse(args) != nil {
		return 2
	}
	for _, item := range []struct {
		path       string
		authorized bool
	}{{"/healthz", false}, {"/readyz", true}} {
		value := ""
		if item.authorized {
			value = *token
		}
		if _, err := get(ctx, strings.TrimRight(*server, "/")+item.path, value); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "bridge health and readiness checks passed")
	return 0
}

func runGet(ctx context.Context, name, path string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	server := flags.String("server", defaultServer, "bridge HTTP base URL")
	token := flags.String("token", os.Getenv("AGENT_BEACON_TOKEN"), "bridge token")
	if flags.Parse(args) != nil {
		return 2
	}
	if *token == "" {
		fmt.Fprintf(stderr, "%s: --token or AGENT_BEACON_TOKEN is required\n", name)
		return 2
	}
	data, err := get(ctx, strings.TrimRight(*server, "/")+path, *token)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	prettyJSON(stdout, data)
	return 0
}

func runEmit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("emit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	server := flags.String("server", defaultServer, "bridge HTTP base URL")
	token := flags.String("token", os.Getenv("AGENT_BEACON_TOKEN"), "bridge token")
	fixture := flags.String("fixture", "", "named mock fixture")
	if flags.Parse(args) != nil {
		return 2
	}
	if *token == "" || *fixture == "" {
		fmt.Fprintln(stderr, "emit: --fixture and --token (or AGENT_BEACON_TOKEN) are required")
		return 2
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(*server, "/")+"/v2/fixtures/"+*fixture, nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	request.Header.Set("X-Agent-Beacon-Token", *token)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer response.Body.Close()
	responseData, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fmt.Fprintf(stderr, "bridge returned %s: %s\n", response.Status, responseData)
		return 1
	}
	prettyJSON(stdout, responseData)
	return 0
}

func get(ctx context.Context, url, token string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		request.Header.Set("X-Agent-Beacon-Token", token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %s", url, response.Status)
	}
	return data, nil
}

func prettyJSON(writer io.Writer, data []byte) {
	var value any
	if json.Unmarshal(data, &value) == nil {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(value)
		return
	}
	_, _ = writer.Write(data)
}

func prettyValue(writer io.Writer, value any) {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}
