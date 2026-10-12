// Command load runs a bounded end-to-end Control/Agent capacity workload.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	agentapp "github.com/senhao-xu/vps-status/internal/agent"
	controlapp "github.com/senhao-xu/vps-status/internal/control"
	"github.com/senhao-xu/vps-status/internal/protocol"
	"github.com/senhao-xu/vps-status/internal/security"
	controlstore "github.com/senhao-xu/vps-status/internal/storage/control"
	"github.com/senhao-xu/vps-status/internal/storage/telemetry"
)

type options struct {
	agents        int
	tasksPerAgent int
	duration      time.Duration
	baseline      bool
	output        string
}

type evidence struct {
	StartedAt       time.Time `json:"started_at"`
	DurationSeconds float64   `json:"duration_seconds"`
	Agents          int       `json:"agents"`
	TCPAssignments  int       `json:"tcp_assignments"`
	OnlineAgents    int       `json:"online_agents"`
	HostSeriesOK    bool      `json:"host_series_ok"`
	ProbeSeriesOK   bool      `json:"probe_series_ok"`
	AlertState      string    `json:"alert_state"`
	MaintenanceOK   bool      `json:"maintenance_ok"`
	TelemetryState  string    `json:"telemetry_state"`
	DroppedWrites   uint64    `json:"dropped_writes"`
	DatabaseBytes   int64     `json:"database_bytes"`
	WALBytes        int64     `json:"wal_bytes"`
	HeapAllocBytes  uint64    `json:"heap_alloc_bytes"`
	SysBytes        uint64    `json:"sys_bytes"`
	GoVersion       string    `json:"go_version"`
	GOOS            string    `json:"goos"`
	GOARCH          string    `json:"goarch"`
	CPUsVisible     int       `json:"cpus_visible"`
	BaselineClaim   bool      `json:"baseline_claim"`
}

type staticCollector struct{ sequence atomic.Uint64 }

func (collector *staticCollector) Collect(now time.Time) (protocol.Report, error) {
	sequence := collector.sequence.Add(1)
	return protocol.Report{
		CollectedAtUnixMilli: now.UnixMilli(),
		CPU:                  protocol.CPUStats{UsagePercent: 50},
		Load:                 protocol.LoadStats{One: 1, Five: 1, Fifteen: 1},
		Memory:               protocol.MemoryStats{TotalBytes: 1024, UsedBytes: 512, AvailableBytes: 512},
		Disk:                 protocol.DiskStats{TotalBytes: 2048, UsedBytes: 1024, FreeBytes: 1024},
		Network: protocol.NetworkStats{ReceiveBytesPerSecond: 100, TransmitBytesPerSecond: 50,
			ReceiveBytesTotal: sequence * 300, TransmitBytesTotal: sequence * 150},
		UptimeSeconds: sequence*3 + 100, ProcessCount: 10, Connections: protocol.ConnectionStats{TCP: 2, UDP: 1},
	}, nil
}

func main() {
	var config options
	flag.IntVar(&config.agents, "agents", 5, "simulated online Agents (1-100)")
	flag.IntVar(&config.tasksPerAgent, "tasks-per-agent", 4, "TCP task assignments per Agent (1-64)")
	flag.DurationVar(&config.duration, "duration", 15*time.Second, "steady workload duration (10s-1h)")
	flag.BoolVar(&config.baseline, "baseline", false, "require the 100-Agent/400-assignment baseline shape and at least 2 minutes")
	flag.StringVar(&config.output, "output", "", "optional JSON evidence path")
	flag.Parse()
	if err := validateOptions(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result, err := run(context.Background(), config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	encoded = append(encoded, '\n')
	if config.output != "" {
		if err := os.WriteFile(config.output, encoded, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "write evidence:", err)
			os.Exit(1)
		}
	}
	_, _ = os.Stdout.Write(encoded)
}

func validateOptions(config options) error {
	if config.agents < 1 || config.agents > 100 || config.tasksPerAgent < 1 || config.tasksPerAgent > protocol.MaxProbeTasks || config.duration < 10*time.Second || config.duration > time.Hour {
		return errors.New("load options exceed bounded agents/tasks/duration limits")
	}
	if config.baseline && (config.agents != 100 || config.tasksPerAgent != 4 || config.duration < 2*time.Minute) {
		return errors.New("--baseline requires --agents 100 --tasks-per-agent 4 and --duration of at least 2m")
	}
	return nil
}

func run(parent context.Context, config options) (result evidence, returnedErr error) {
	started := time.Now().UTC()
	directory, err := os.MkdirTemp("", "vps-status-load-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(directory)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	store, err := controlstore.Open(ctx, filepath.Join(directory, "control.db"))
	if err != nil {
		return result, err
	}
	defer store.Close()
	key, err := security.LoadOrCreateMasterKey(filepath.Join(directory, "master.key"))
	if err != nil {
		return result, err
	}
	telemetryStore, err := telemetry.Open(ctx, filepath.Join(directory, "telemetry.db"))
	if err != nil {
		return result, err
	}
	service, err := telemetry.NewService(telemetryStore, telemetry.Config{QueueCapacity: 4096, FileBudgetBytes: 512 << 20, MinimumFreeBytes: 1})
	if err != nil {
		_ = telemetryStore.Close()
		return result, err
	}
	defer service.Close()

	nodeIDs, credentials, err := createNodes(ctx, store, config.agents, started)
	if err != nil {
		return result, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	tcpDone := make(chan struct{})
	go func() {
		defer close(tcpDone)
		acceptTCP(ctx, listener)
	}()
	defer func() {
		_ = listener.Close()
		<-tcpDone
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	for taskIndex := 0; taskIndex < config.tasksPerAgent; taskIndex++ {
		task := controlstore.ProbeTask{ID: fmt.Sprintf("load-task-%d", taskIndex), Name: fmt.Sprintf("Load task %d", taskIndex), Host: "127.0.0.1", Port: uint16(port), IntervalSeconds: 5, CreatedAt: started, UpdatedAt: started}
		if _, err := store.CreateProbeTask(ctx, task, nodeIDs, started); err != nil {
			return result, err
		}
	}
	if err := store.CreateAlertRule(ctx, controlstore.AlertRule{ID: "load-cpu-alert", Name: "Load CPU alert", ScopeKind: "node", ScopeValue: nodeIDs[0], Metric: "cpu", Threshold: 10, Enabled: true, CreatedAt: started, UpdatedAt: started}); err != nil {
		return result, err
	}

	live := controlapp.NewLiveStore()
	history := controlapp.NewHostHistory(service)
	probeHistory := controlapp.NewProbeHistory(service)
	dispatcher, err := controlapp.NewNotificationDispatcher(ctx, store, key, controlapp.NotificationDispatcherConfig{})
	if err != nil {
		return result, err
	}
	alerts, err := controlapp.NewAlertManager(ctx, store, live, dispatcher, controlapp.AlertManagerConfig{QueueSize: 512, TickInterval: time.Second, StartupGrace: time.Hour})
	if err != nil {
		cancel()
		dispatcher.Wait()
		return result, err
	}
	defer func() {
		cancel()
		alerts.Wait()
		dispatcher.Wait()
	}()
	handler, err := controlapp.NewHandler(controlapp.APIConfig{Store: store, MasterKey: key, Telemetry: service, History: history, ProbeHistory: probeHistory, Live: live, Alerts: alerts, Notifications: dispatcher, WorkerContext: ctx})
	if err != nil {
		return result, err
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	var clients sync.WaitGroup
	clientErrors := make(chan error, config.agents)
	for index := range nodeIDs {
		client := &agentapp.Client{Config: agentapp.Config{ControlURL: server.URL, NodeID: nodeIDs[index], Credential: credentials[index]}, AgentVersion: "load", Collector: &staticCollector{}}
		clients.Add(1)
		go func() {
			defer clients.Done()
			if err := client.ConnectOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				clientErrors <- err
			}
		}()
	}
	defer func() {
		cancel()
		clients.Wait()
	}()
	if err := waitOnline(ctx, live, nodeIDs, 30*time.Second); err != nil {
		return result, err
	}
	select {
	case <-time.After(config.duration):
	case err := <-clientErrors:
		return result, fmt.Errorf("Agent workload failed: %w", err)
	case <-ctx.Done():
		return result, ctx.Err()
	}

	sealAt := time.Now().UTC().Add(time.Minute)
	history.Seal(sealAt)
	probeHistory.Seal(sealAt)
	hostOK, probeOK := waitHistory(ctx, service, nodeIDs, config.tasksPerAgent, started, sealAt, 10*time.Second)
	maintenanceContext, maintenanceCancel := context.WithTimeout(ctx, 30*time.Second)
	_, maintenanceErr := service.RunMaintenance(maintenanceContext, time.Now().UTC())
	maintenanceCancel()
	stats, statsErr := service.Stats(ctx)
	if statsErr != nil {
		return result, statsErr
	}
	alertState, err := store.AlertState(ctx, "load-cpu-alert", nodeIDs[0])
	if err != nil {
		return result, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	online := 0
	for _, nodeID := range nodeIDs {
		if live.Snapshot(nodeID).Online {
			online++
		}
	}
	state := service.State()
	stateText := "writable"
	if !state.Writable {
		stateText = state.Reason
	}
	result = evidence{StartedAt: started, DurationSeconds: time.Since(started).Seconds(), Agents: config.agents,
		TCPAssignments: config.agents * config.tasksPerAgent, OnlineAgents: online, HostSeriesOK: hostOK,
		ProbeSeriesOK: probeOK, AlertState: alertState.Status, MaintenanceOK: maintenanceErr == nil,
		TelemetryState: stateText, DroppedWrites: state.Dropped, DatabaseBytes: stats.MainBytes, WALBytes: stats.WALBytes,
		HeapAllocBytes: memory.HeapAlloc, SysBytes: memory.Sys, GoVersion: runtime.Version(), GOOS: runtime.GOOS,
		GOARCH: runtime.GOARCH, CPUsVisible: runtime.NumCPU(), BaselineClaim: config.baseline}
	if online != config.agents || !hostOK || !probeOK || alertState.Status != "firing" || maintenanceErr != nil || !state.Writable || state.Dropped != 0 {
		return result, errors.New("load acceptance failed: one or more connection/history/probe/alert/maintenance checks did not pass")
	}
	return result, nil
}

func createNodes(ctx context.Context, store *controlstore.Store, count int, now time.Time) ([]string, []string, error) {
	nodes := make([]string, 0, count)
	credentials := make([]string, 0, count)
	for index := 0; index < count; index++ {
		nodeID := "load-node-" + strconv.Itoa(index)
		credential := "load-credential-" + strconv.Itoa(index)
		tokenValue := "load-enrollment-" + strconv.Itoa(index)
		token := controlstore.EnrollmentToken{ID: "load-token-" + strconv.Itoa(index), Digest: security.DigestEnrollmentToken(tokenValue), NodeName: nodeID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		if err := store.CreateEnrollmentToken(ctx, token); err != nil {
			return nil, nil, err
		}
		if _, err := store.ConsumeEnrollment(ctx, token.Digest, nodeID, "load-credential-id-"+strconv.Itoa(index), security.DigestNodeCredential(credential), now); err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, nodeID)
		credentials = append(credentials, credential)
	}
	return nodes, credentials, nil
}

func acceptTCP(ctx context.Context, listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		_ = connection.Close()
		if ctx.Err() != nil {
			return
		}
	}
}

func waitOnline(ctx context.Context, live *controlapp.LiveStore, nodeIDs []string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		online := 0
		for _, nodeID := range nodeIDs {
			if live.Snapshot(nodeID).Online {
				online++
			}
		}
		if online == len(nodeIDs) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("only %d/%d Agents became online", online, len(nodeIDs))
		case <-ticker.C:
		}
	}
}

func waitHistory(ctx context.Context, service *telemetry.Service, nodeIDs []string, tasksPerAgent int, from, to time.Time, timeout time.Duration) (bool, bool) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	hostReady := make(map[string]bool, len(nodeIDs))
	probeReady := make(map[string]bool, len(nodeIDs)*tasksPerAgent)
	for {
		for _, nodeID := range nodeIDs {
			if !hostReady[nodeID] {
				host, err := service.QueryHost(ctx, nodeID, telemetry.Resolution1m, from.Add(-time.Minute), to.Add(time.Minute))
				hostReady[nodeID] = err == nil && len(host) > 0
			}
			for taskIndex := 0; taskIndex < tasksPerAgent; taskIndex++ {
				taskID := "load-task-" + strconv.Itoa(taskIndex)
				seriesID := nodeID + "\x00" + taskID
				if probeReady[seriesID] {
					continue
				}
				probe, err := service.QueryProbe(ctx, nodeID, taskID, telemetry.Resolution1m, from.Add(-time.Minute), to.Add(time.Minute))
				probeReady[seriesID] = err == nil && len(probe) > 0 && probe[0].Attempts > 0
			}
		}
		hostOK := len(hostReady) == len(nodeIDs)
		for _, ready := range hostReady {
			hostOK = hostOK && ready
		}
		probeOK := len(probeReady) == len(nodeIDs)*tasksPerAgent
		for _, ready := range probeReady {
			probeOK = probeOK && ready
		}
		if hostOK && probeOK {
			return true, true
		}
		select {
		case <-ctx.Done():
			return hostOK, probeOK
		case <-deadline.C:
			return hostOK, probeOK
		case <-ticker.C:
		}
	}
}
