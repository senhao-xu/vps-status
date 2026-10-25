// Command agent is the outbound-only VPS Status Linux Agent entrypoint.
package main

import (
	"context"
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

	agentapp "github.com/senhao-xu/vps-status/internal/agent"
	"github.com/senhao-xu/vps-status/internal/security"
	"github.com/senhao-xu/vps-status/internal/versioninfo"
)

const defaultConfigPath = "/etc/vps-status/agent.json"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 && arguments[0] == "credential" {
		if err := runCredential(arguments[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "agent credential update failed: %s\n", security.Redact(err.Error()))
			return 1
		}
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "enroll" {
		if err := runEnroll(arguments[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "agent enrollment failed: %s\n", security.Redact(err.Error()))
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfigPath, "Agent configuration path")
	showVersion := flags.Bool("version", false, "print build information and exit")
	sourcePaths := sourcePathFlags(flags)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, versioninfo.String("agent"))
		return 0
	}
	if err := sourcePaths.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !agentapp.SupportedPlatform() {
		fmt.Fprintln(stderr, "agent startup failed: host collection is supported only on Linux")
		return 1
	}
	config, err := agentapp.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "agent startup failed: %s\n", security.Redact(err.Error()))
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &agentapp.Client{Config: config, AgentVersion: versioninfo.Version, SourcePaths: *sourcePaths}
	err = client.Run(ctx, agentapp.RetryPolicy{})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "agent stopped: %s\n", security.Redact(err.Error()))
		return 1
	}
	return 0
}

func sourcePathFlags(flags *flag.FlagSet) *agentapp.SourcePaths {
	paths := agentapp.DefaultSourcePaths()
	flags.StringVar(&paths.ProcRoot, "proc-root", paths.ProcRoot, "procfs root for host collection")
	flags.StringVar(&paths.SysRoot, "sys-root", paths.SysRoot, "sysfs root for host collection")
	flags.StringVar(&paths.RootFS, "rootfs", paths.RootFS, "root filesystem path for host disk usage")
	flags.StringVar(&paths.OSRelease, "os-release", paths.OSRelease, "host os-release file path")
	return &paths
}

func runCredential(arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "replace" {
		return errors.New("usage: agent credential replace --credential-file PATH [--config PATH]")
	}
	flags := flag.NewFlagSet("agent credential replace", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaultConfigPath, "Agent configuration path")
	credentialPath := flags.String("credential-file", "", "mode-0600 file containing the new credential")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *credentialPath == "" {
		return errors.New("usage: agent credential replace --credential-file PATH [--config PATH]")
	}
	credentialInfo, err := os.Stat(*credentialPath)
	if err != nil || !credentialInfo.Mode().IsRegular() || credentialInfo.Mode().Perm()&0o077 != 0 || credentialInfo.Size() < 1 || credentialInfo.Size() > 4096 {
		return errors.New("credential file must be a bounded mode-0600 regular file")
	}
	credential, err := os.ReadFile(*credentialPath)
	if err != nil {
		return errors.New("read credential file")
	}
	config, err := agentapp.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	config.Credential = strings.TrimSpace(string(credential))
	if err := agentapp.SaveConfig(*configPath, config); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Agent credential replaced; restart the service to reconnect.")
	return nil
}

func runEnroll(arguments []string, stdout, stderr io.Writer) error {
	if !agentapp.SupportedPlatform() {
		return errors.New("Agent enrollment is supported only on Linux")
	}
	flags := flag.NewFlagSet("agent enroll", flag.ContinueOnError)
	flags.SetOutput(stderr)
	controlURL := flags.String("control-url", "", "public HTTPS Control URL")
	token := flags.String("token", "", "one-time enrollment token")
	configPath := flags.String("config", defaultConfigPath, "Agent configuration path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *controlURL == "" || *token == "" {
		return errors.New("usage: vps-status-agent enroll --control-url URL --token TOKEN [--config PATH]")
	}
	config, err := agentapp.Enroll(context.Background(), &http.Client{Timeout: 30 * time.Second}, *controlURL, *token)
	if err != nil {
		return err
	}
	if err := agentapp.SaveConfig(*configPath, config); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Agent enrolled as node %s; credential saved to %s.\n", config.NodeID, *configPath)
	return nil
}
