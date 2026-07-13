package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const keychainService = "com.stepatero.agentbeacon"

func Get(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("secret name is required")
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password",
		"-s", keychainService, "-a", name, "-w")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("read Keychain secret %q: %w", name, err)
	}
	value := strings.TrimSpace(stdout.String())
	if value == "" {
		return "", fmt.Errorf("Keychain secret %q is empty", name)
	}
	return value, nil
}

func Set(ctx context.Context, name, value string) error {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" || value == "" {
		return errors.New("secret name and value are required")
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", "add-generic-password", "-U",
		"-s", keychainService, "-a", name, "-w", value)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("write Keychain secret %q: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func Delete(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("secret name is required")
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", "delete-generic-password",
		"-s", keychainService, "-a", name)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("delete Keychain secret %q: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
