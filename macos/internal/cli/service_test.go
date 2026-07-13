package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderLaunchAgentUsesAbsoluteStablePathsAndEscapesXML(t *testing.T) {
	home := filepath.Join(t.TempDir(), "User & Test")
	paths := defaultServicePaths(home)
	plist := renderLaunchAgent(paths, home)
	for _, required := range []string{
		"com.stepatero.agentbeacon", "<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>",
		"/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", "User &amp; Test", "agent-beacon-bridge",
	} {
		if !strings.Contains(plist, required) {
			t.Fatalf("plist missing %q: %s", required, plist)
		}
	}
}

func TestCopyFileSetsPrivateMode(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.WriteFile(source, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(source, destination, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode().Perm(), err)
	}
}
