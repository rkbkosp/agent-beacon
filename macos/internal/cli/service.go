package cli

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-beacon/internal/config"
	"agent-beacon/internal/secrets"
)

const launchAgentLabel = "com.stepatero.agentbeacon"

type servicePaths struct {
	ApplicationDir string
	Binary         string
	Config         string
	Token          string
	Logs           string
	Plist          string
}

func runInstallService(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install-service", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configSource := flags.String("config", "configs/config.local.yaml", "source configuration file")
	tokenSource := flags.String("token-file", "configs/token.local", "source bridge token file")
	if flags.Parse(args) != nil {
		return 2
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	paths := defaultServicePaths(home)
	for _, directory := range []string{filepath.Dir(paths.Binary), paths.Logs, filepath.Dir(paths.Plist)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if err := copyFile(executable, paths.Binary, 0o755); err != nil {
		fmt.Fprintf(stderr, "install bridge binary: %v\n", err)
		return 1
	}
	if err := copyFile(*configSource, paths.Config, 0o600); err != nil {
		fmt.Fprintf(stderr, "install bridge config: %v\n", err)
		return 1
	}
	if err := copyFile(*tokenSource, paths.Token, 0o600); err != nil {
		fmt.Fprintf(stderr, "install bridge token: %v\n", err)
		return 1
	}
	settings, err := config.Load(paths.Config)
	if err != nil {
		fmt.Fprintf(stderr, "validate installed config: %v\n", err)
		return 1
	}
	plist := renderLaunchAgent(paths, home)
	if err := writeAtomic(paths.Plist, []byte(plist), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if output, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-lint", paths.Plist).CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, "validate LaunchAgent: %v: %s\n", err, strings.TrimSpace(string(output)))
		return 1
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	service := domain + "/" + launchAgentLabel
	_ = exec.CommandContext(ctx, "/bin/launchctl", "bootout", service).Run()
	if err := waitForLaunchAgentRemoval(ctx, service, 5*time.Second); err != nil {
		fmt.Fprintf(stderr, "wait for previous LaunchAgent removal: %v\n", err)
		return 1
	}
	if err := bootstrapLaunchAgent(ctx, domain, paths.Plist, 5); err != nil {
		fmt.Fprintf(stderr, "launchctl bootstrap: %v\n", err)
		return 1
	}
	_ = exec.CommandContext(ctx, "/bin/launchctl", "enable", service).Run()
	if output, err := exec.CommandContext(ctx, "/bin/launchctl", "kickstart", "-k", service).CombinedOutput(); err != nil {
		fmt.Fprintf(stderr, "launchctl kickstart: %v: %s\n", err, strings.TrimSpace(string(output)))
		return 1
	}
	tokenData, _ := os.ReadFile(paths.Token)
	if err := waitForReady(ctx, settings.Server.Listen, strings.TrimSpace(string(tokenData)), 15*time.Second); err != nil {
		fmt.Fprintf(stderr, "LaunchAgent loaded but readiness failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Installed and started %s\n", launchAgentLabel)
	fmt.Fprintf(stdout, "Config: %s\nLogs: %s\n", paths.Config, paths.Logs)
	return 0
}

func waitForLaunchAgentRemoval(ctx context.Context, service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if exec.CommandContext(ctx, "/bin/launchctl", "print", service).Run() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s remained loaded for %s", service, timeout)
}

func bootstrapLaunchAgent(ctx context.Context, domain, plist string, attempts int) error {
	var lastError error
	for attempt := 1; attempt <= attempts; attempt++ {
		output, err := exec.CommandContext(ctx, "/bin/launchctl", "bootstrap", domain, plist).CombinedOutput()
		if err == nil {
			return nil
		}
		lastError = fmt.Errorf("attempt %d/%d: %w: %s", attempt, attempts, err, strings.TrimSpace(string(output)))
		if exec.CommandContext(ctx, "/bin/launchctl", "print", domain+"/"+launchAgentLabel).Run() == nil {
			return nil
		}
		if attempt < attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return lastError
}

func runUninstallService(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uninstall-service", flag.ContinueOnError)
	flags.SetOutput(stderr)
	purge := flags.Bool("purge", false, "also remove local data, logs, and Keychain secrets")
	if flags.Parse(args) != nil {
		return 2
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	paths := defaultServicePaths(home)
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.CommandContext(ctx, "/bin/launchctl", "bootout", domain+"/"+launchAgentLabel).Run()
	if err := os.Remove(paths.Plist); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *purge {
		_ = os.RemoveAll(paths.ApplicationDir)
		_ = os.RemoveAll(paths.Logs)
		_ = secrets.Delete(ctx, "zero-api-key")
	}
	fmt.Fprintf(stdout, "Uninstalled %s (data preserved: %t)\n", launchAgentLabel, !*purge)
	return 0
}

func defaultServicePaths(home string) servicePaths {
	application := filepath.Join(home, "Library", "Application Support", "AgentBeacon")
	return servicePaths{
		ApplicationDir: application,
		Binary:         filepath.Join(application, "bin", "agent-beacon-bridge"),
		Config:         filepath.Join(application, "config.yaml"),
		Token:          filepath.Join(application, "token"),
		Logs:           filepath.Join(home, "Library", "Logs", "AgentBeacon"),
		Plist:          filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"),
	}
}

func renderLaunchAgent(paths servicePaths, home string) string {
	escape := func(value string) string {
		var output bytes.Buffer
		_ = xml.EscapeText(&output, []byte(value))
		return output.String()
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>WorkingDirectory</key><string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key><string>%s</string>
    <key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, launchAgentLabel, escape(paths.Binary), escape(paths.Config), escape(paths.ApplicationDir),
		escape(home), escape(filepath.Join(paths.Logs, "stdout.log")), escape(filepath.Join(paths.Logs, "stderr.log")))
}

func copyFile(source, destination string, mode os.FileMode) error {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if sourcePath == destinationPath {
		return os.Chmod(destinationPath, mode)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return writeAtomic(destinationPath, data, mode)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-beacon-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func waitForReady(ctx context.Context, listen, token string, timeout time.Duration) error {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("parse server.listen: %w", err)
	}
	url := "http://127.0.0.1:" + port + "/readyz"
	deadline := time.Now().Add(timeout)
	var lastError error
	for time.Now().Before(deadline) {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		request.Header.Set("X-Agent-Beacon-Token", token)
		response, requestErr := (&http.Client{Timeout: time.Second}).Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastError = fmt.Errorf("readyz returned %s", response.Status)
		} else {
			lastError = requestErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("readyz did not succeed within %s: %w", timeout, lastError)
}
