// Command control starts the VPS Status Control service and provides local
// data-directory administration commands.
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
	"path/filepath"
	"syscall"
	"time"

	"github.com/senhao-xu/vps-status/internal/backup"
	controlapp "github.com/senhao-xu/vps-status/internal/control"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
	"github.com/senhao-xu/vps-status/internal/versioninfo"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		if err := runHealthcheck(arguments[1:], stderr); err != nil {
			fmt.Fprintf(stderr, "control healthcheck failed: %s\n", security.Redact(err.Error()))
			return 1
		}
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "backup" {
		if err := runBackup(arguments[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "control backup failed: %s\n", security.Redact(err.Error()))
			return 1
		}
		return 0
	}
	if len(arguments) > 0 && arguments[0] == "admin" {
		if err := runAdmin(arguments[1:], stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "control admin failed: %s\n", security.Redact(err.Error()))
			return 1
		}
		return 0
	}
	if err := runServer(arguments, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "control server failed: %s\n", security.Redact(err.Error()))
		return 1
	}
	return 0
}

func runHealthcheck(arguments []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("control healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	endpoint := flags.String("url", "http://127.0.0.1:8080/readyz", "readiness URL")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: control healthcheck [--url URL]")
	}
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, *endpoint, nil)
	if err != nil {
		return errors.New("invalid readiness URL")
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("readiness endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness endpoint returned status %d", response.StatusCode)
	}
	return nil
}

func runBackup(arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("usage: control backup create|verify|restore [options]")
	}
	switch arguments[0] {
	case "create":
		flags := flag.NewFlagSet("control backup create", flag.ContinueOnError)
		flags.SetOutput(stderr)
		dataDirectory := flags.String("data-dir", "./data", "Control data directory")
		output := flags.String("output", "", "backup archive path")
		includeTelemetry := flags.Bool("include-telemetry", false, "include the independently recoverable telemetry database")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *output == "" {
			return errors.New("usage: control backup create --data-dir PATH --output FILE [--include-telemetry]")
		}
		manifest, err := backup.Create(context.Background(), backup.Options{DataDirectory: *dataDirectory, OutputPath: *output, IncludeTelemetry: *includeTelemetry, Version: versioninfo.Version})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Backup created: %s (%d files, telemetry=%t).\n", *output, len(manifest.Files), manifest.IncludeTelemetry)
		return nil
	case "verify":
		flags := flag.NewFlagSet("control backup verify", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "backup archive path")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *input == "" {
			return errors.New("usage: control backup verify --input FILE")
		}
		manifest, err := backup.Verify(context.Background(), *input)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Backup verified: %s (%d files, created %s).\n", *input, len(manifest.Files), manifest.CreatedAt.Format(time.RFC3339))
		return nil
	case "restore":
		flags := flag.NewFlagSet("control backup restore", flag.ContinueOnError)
		flags.SetOutput(stderr)
		input := flags.String("input", "", "backup archive path")
		dataDirectory := flags.String("data-dir", "", "new restore data directory that does not exist")
		if err := flags.Parse(arguments[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *input == "" || *dataDirectory == "" {
			return errors.New("usage: control backup restore --input FILE --data-dir NEW_PATH")
		}
		manifest, err := backup.Restore(context.Background(), *input, *dataDirectory)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Backup restored and verified into %s (%d files).\n", *dataDirectory, len(manifest.Files))
		return nil
	default:
		return errors.New("usage: control backup create|verify|restore [options]")
	}
}

func runServer(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("control", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listenAddress := flags.String("listen", ":8080", "HTTP listen address")
	dataDirectory := flags.String("data-dir", "./data", "Control data directory")
	showVersion := flags.Bool("version", false, "print build information and exit")
	telemetryBudget := flags.Int64("telemetry-max-bytes", 512<<20, "maximum telemetry DB/WAL/SHM bytes")
	telemetryMinFree := flags.Uint64("telemetry-min-free-bytes", 256<<20, "minimum free bytes before history pauses")
	telemetryRetentionDays := flags.Int("telemetry-retention-days", 30, "hourly history retention: 30, 90, or 365 days")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if *showVersion {
		fmt.Fprintln(stdout, versioninfo.String("control"))
		return nil
	}
	if *telemetryBudget <= 0 {
		return errors.New("telemetry maximum bytes must be positive")
	}

	masterKey, err := security.LoadOrCreateMasterKey(filepath.Join(*dataDirectory, "master.key"))
	if err != nil {
		return fmt.Errorf("load deployment key: %w", err)
	}
	store, err := controlstore.Open(context.Background(), filepath.Join(*dataDirectory, "control.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	retention := telemetry.DefaultRetention
	if *telemetryRetentionDays != 30 {
		retention, err = telemetry.ExtendedRetention(*telemetryRetentionDays)
		if err != nil {
			return err
		}
	}
	telemetryStore, telemetryErr := telemetry.Open(context.Background(), filepath.Join(*dataDirectory, "telemetry.db"))
	var telemetryService *telemetry.Service
	if telemetryErr != nil {
		fmt.Fprintln(stderr, "warning: telemetry history unavailable; Control will continue without history (open_failed)")
		telemetryService = telemetry.NewUnavailable("open_failed")
	} else {
		telemetryService, telemetryErr = telemetry.NewService(telemetryStore, telemetry.Config{
			FileBudgetBytes: *telemetryBudget, MinimumFreeBytes: *telemetryMinFree, Retention: retention,
			StateChange: func(state telemetry.State) {
				if state.Writable {
					fmt.Fprintln(stderr, "telemetry history recovered")
				} else {
					fmt.Fprintf(stderr, "warning: telemetry history paused: %s\n", state.Reason)
				}
			},
		})
		if telemetryErr != nil {
			_ = telemetryStore.Close()
			fmt.Fprintln(stderr, "warning: telemetry history unavailable; Control will continue without history (configuration_invalid)")
			telemetryService = telemetry.NewUnavailable("configuration_invalid")
		}
	}
	defer telemetryService.Close()
	history := controlapp.NewHostHistory(telemetryService)
	probeHistory := controlapp.NewProbeHistory(telemetryService)
	workerContext, stopWorkers := context.WithCancel(context.Background())
	live := controlapp.NewLiveStore()
	registry := controlapp.NewRegistry()
	dispatcher, err := controlapp.NewNotificationDispatcher(workerContext, store, masterKey, controlapp.NotificationDispatcherConfig{})
	if err != nil {
		stopWorkers()
		return fmt.Errorf("create notification dispatcher: %w", err)
	}
	alerts, err := controlapp.NewAlertManager(workerContext, store, live, dispatcher, controlapp.AlertManagerConfig{})
	if err != nil {
		stopWorkers()
		dispatcher.Wait()
		return fmt.Errorf("create alert manager: %w", err)
	}
	defer func() {
		registry.DisconnectAll("server_shutdown")
		stopWorkers()
		alerts.Wait()
		dispatcher.Wait()
	}()
	go maintainTelemetry(workerContext, store, history, probeHistory, telemetryService)
	handler, err := controlapp.NewHandler(controlapp.APIConfig{Store: store, MasterKey: masterKey, Telemetry: telemetryService,
		History: history, ProbeHistory: probeHistory, Live: live, Registry: registry, Notifications: dispatcher, Alerts: alerts, WorkerContext: workerContext})
	if err != nil {
		return fmt.Errorf("create Control handler: %w", err)
	}
	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Fprintf(stdout, "VPS Status Control listening on %s\n", *listenAddress)
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	shutdownContext, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful HTTP shutdown: %w", err)
		}
		err := <-serverErrors
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func maintainTelemetry(ctx context.Context, store *controlstore.Store, history *controlapp.HostHistory, probes *controlapp.ProbeHistory, service *telemetry.Service) {
	runTelemetryCleanup(ctx, store, service, time.Now().UTC())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			runTelemetryCleanup(ctx, store, service, now)
			history.Seal(now)
			probes.Seal(now)
			if now.Minute() == 0 {
				maintenanceCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				_, _ = service.RunMaintenance(maintenanceCtx, now)
				cancel()
			}
		}
	}
}

// runTelemetryCleanup performs one bounded durable retry pass. A process
// crash, unavailable telemetry store, or failed deletion leaves the job in
// control.db for the next startup or minute tick.
func runTelemetryCleanup(ctx context.Context, store *controlstore.Store, service *telemetry.Service, now time.Time) {
	if store == nil || service == nil {
		return
	}
	jobs, err := store.ListTelemetryCleanupJobs(ctx, 64)
	if err != nil {
		return
	}
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := store.MarkTelemetryCleanupAttempt(ctx, job.NodeID, now); err != nil {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := service.DeleteNode(cleanupCtx, job.NodeID)
		cancel()
		if err != nil {
			continue
		}
		_ = store.CompleteTelemetryCleanup(ctx, job.NodeID)
	}
	probeJobs, err := store.ListProbeCleanupJobs(ctx, 64)
	if err != nil {
		return
	}
	for _, job := range probeJobs {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := store.MarkProbeCleanupAttempt(ctx, job.TaskID, now); err != nil {
			continue
		}
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := service.DeleteProbeTask(cleanupCtx, job.TaskID)
		cancel()
		if err != nil {
			continue
		}
		_ = store.CompleteProbeCleanup(ctx, job.TaskID)
	}
}

func runAdmin(arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 || arguments[0] != "reset-mfa" {
		return errors.New("usage: control admin reset-mfa --data-dir PATH")
	}
	flags := flag.NewFlagSet("control admin reset-mfa", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDirectory := flags.String("data-dir", "./data", "Control data directory")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	databasePath := filepath.Join(*dataDirectory, "control.db")
	if _, err := os.Stat(databasePath); err != nil {
		return fmt.Errorf("access control database: %w", err)
	}
	store, err := controlstore.Open(context.Background(), databasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.ResetMFA(context.Background(), time.Now().UTC()); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Administrator MFA reset; all sessions were revoked.")
	return nil
}
