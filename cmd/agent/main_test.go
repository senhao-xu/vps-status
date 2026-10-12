package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	agentapp "github.com/senhao-xu/vps-status/internal/agent"
)

func TestCredentialReplaceUsesRestrictedFileAndAtomicConfig(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "agent.json")
	if err := agentapp.SaveConfig(configPath, agentapp.Config{ControlURL: "http://127.0.0.1", NodeID: "node", Credential: "old"}); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(directory, "credential")
	if err := os.WriteFile(credentialPath, []byte("new-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run([]string{"credential", "replace", "--config", configPath, "--credential-file", credentialPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("run()=%d stderr=%q", exit, stderr.String())
	}
	config, err := agentapp.LoadConfig(configPath)
	if err != nil || config.Credential != "new-secret" {
		t.Fatalf("LoadConfig()=%#v, %v", config, err)
	}
	if err := os.Chmod(credentialPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if exit := run([]string{"credential", "replace", "--config", configPath, "--credential-file", credentialPath}, &stdout, &stderr); exit == 0 {
		t.Fatal("group-readable credential file accepted")
	}
}
