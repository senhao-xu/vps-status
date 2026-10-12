// Package agent implements the Linux Agent's outbound-only Control client.
package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxConfigBytes = 16 << 10

// Config contains the one node credential needed for outbound WSS.
type Config struct {
	ControlURL string `json:"control_url"`
	NodeID     string `json:"node_id"`
	Credential string `json:"credential"`
}

// LoadConfig reads a mode-0600 Agent configuration.
func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open Agent config: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Config{}, fmt.Errorf("inspect Agent config: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, errors.New("Agent config must not be readable by group or other users")
	}
	if !info.Mode().IsRegular() {
		return Config{}, errors.New("Agent config must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read Agent config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, errors.New("Agent config exceeds the size limit")
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, errors.New("Agent config is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("Agent config is invalid")
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

// SaveConfig atomically persists the credential for the service user only.
func SaveConfig(path string, config Config) error {
	if err := validateConfig(config); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Agent config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Agent config directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-config-*")
	if err != nil {
		return fmt.Errorf("create temporary Agent config: %w", err)
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict temporary Agent config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write Agent config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync Agent config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Agent config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Agent config: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open Agent config directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("sync Agent config directory: %w", errors.Join(syncErr, closeErr))
	}
	ok = true
	return nil
}

func validateConfig(config Config) error {
	if config.ControlURL == "" || config.NodeID == "" || config.Credential == "" {
		return errors.New("Agent config is incomplete")
	}
	if _, err := websocketURL(config.ControlURL); err != nil {
		return errors.New("Agent config Control URL is invalid")
	}
	return nil
}
