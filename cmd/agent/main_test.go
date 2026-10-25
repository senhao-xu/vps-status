package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestSourcePathFlagsDefaultToNativeAndAcceptContainerMounts(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		want      agentapp.SourcePaths
	}{
		{"native defaults", nil, agentapp.SourcePaths{ProcRoot: "/proc", SysRoot: "/sys", RootFS: "/", OSRelease: "/etc/os-release"}},
		{"container mounts", []string{"--proc-root", "/host/proc", "--sys-root", "/host/sys", "--rootfs", "/host/root", "--os-release", "/host/os-release"},
			agentapp.SourcePaths{ProcRoot: "/host/proc", SysRoot: "/host/sys", RootFS: "/host/root", OSRelease: "/host/os-release"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			paths := sourcePathFlags(flags)
			if err := flags.Parse(test.arguments); err != nil {
				t.Fatal(err)
			}
			if *paths != test.want {
				t.Fatalf("source paths=%#v want %#v", *paths, test.want)
			}
			if err := paths.Validate(); err != nil {
				t.Fatalf("valid operator source paths rejected: %v", err)
			}
		})
	}
}

func TestRunRejectsInvalidSourcePathBeforeReadingCredentials(t *testing.T) {
	for _, name := range []string{"--proc-root", "--sys-root", "--rootfs", "--os-release"} {
		for _, value := range []string{"", "relative/path", "/host/invalid\x00path"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				exit := run([]string{"--config", filepath.Join(t.TempDir(), "missing.json"), name, value}, &stdout, &stderr)
				if exit != 2 || !strings.Contains(stderr.String(), name+" must be a non-empty absolute path") || stdout.Len() != 0 {
					t.Fatalf("invalid source path exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
				}
			})
		}
	}
}
