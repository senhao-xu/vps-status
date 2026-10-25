//go:build linux

package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/senhao-xu/vps-status/internal/protocol"
)

const sourceFixtureBootID = "550e8400-e29b-41d4-a716-446655440000"

func hostSourceFixture(t *testing.T) SourcePaths {
	t.Helper()
	directory := t.TempDir()
	paths := SourcePaths{
		ProcRoot: filepath.Join(directory, "host-proc"), SysRoot: filepath.Join(directory, "host-sys"),
		RootFS: filepath.Join(directory, "host-root"), OSRelease: filepath.Join(directory, "host-os-release"),
	}
	for _, path := range []string{paths.RootFS, filepath.Join(paths.ProcRoot, "42")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(paths.ProcRoot, "stat"):                                "cpu 100 0 100 800 0 0 0 0\n",
		filepath.Join(paths.ProcRoot, "loadavg"):                             "0.10 0.20 0.30 1/20 42\n",
		filepath.Join(paths.ProcRoot, "meminfo"):                             "MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 100 kB\nSwapFree: 40 kB\n",
		filepath.Join(paths.ProcRoot, "uptime"):                              "123.9 0.0\n",
		filepath.Join(paths.ProcRoot, "net", "dev"):                          "Inter-| Receive | Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\nethup: 100 0 0 0 0 0 0 0 200 0 0 0 0 0 0 0\nethdown: 900 0 0 0 0 0 0 0 800 0 0 0 0 0 0 0\n",
		filepath.Join(paths.ProcRoot, "net", "tcp"):                          "header\nrow\n",
		filepath.Join(paths.ProcRoot, "net", "udp"):                          "header\nrow\nrow\n",
		filepath.Join(paths.ProcRoot, "1", "cgroup"):                         "0::/system.slice\n",
		filepath.Join(paths.ProcRoot, "sys", "kernel", "osrelease"):          "fixture-kernel\n",
		filepath.Join(paths.ProcRoot, "sys", "kernel", "random", "boot_id"):  sourceFixtureBootID + "\n",
		filepath.Join(paths.SysRoot, "class", "net", "ethup", "operstate"):   "up\n",
		filepath.Join(paths.SysRoot, "class", "net", "ethdown", "operstate"): "down\n",
		filepath.Join(paths.SysRoot, "class", "dmi", "id", "product_name"):   "QEMU fixture\n",
		paths.OSRelease: "PRETTY_NAME=\"Fixture Host OS 42\"\n",
	}
	for path, value := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func TestCollectorAndHostFactsUseConfiguredSources(t *testing.T) {
	paths := hostSourceFixture(t)
	report, err := NewCollectorWithPaths(paths).Collect(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unavailable) != 0 || report.CPU.UsagePercent != 20 || report.Load.One != 0.1 ||
		report.Memory.UsedBytes != 600*1024 || report.UptimeSeconds != 123 || report.ProcessCount != 2 ||
		report.Connections.TCP != 1 || report.Connections.UDP != 2 ||
		report.Network.ReceiveBytesTotal != 100 || report.Network.TransmitBytesTotal != 200 {
		t.Fatalf("report did not use configured procfs/sysfs: %#v", report)
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(paths.RootFS, &stats); err != nil {
		t.Fatal(err)
	}
	if report.Disk.TotalBytes != stats.Blocks*uint64(stats.Bsize) {
		t.Fatalf("disk total=%d want %d", report.Disk.TotalBytes, stats.Blocks*uint64(stats.Bsize))
	}
	hostname, _ := os.Hostname()
	facts := CollectHostFactsWithPaths(paths)
	if facts.Hostname != hostname || facts.OS != "Fixture Host OS 42" || facts.Kernel != "fixture-kernel" ||
		facts.Virtualization != "kvm" || facts.BootID != sourceFixtureBootID {
		t.Fatalf("facts did not use configured host sources and current UTS hostname: %#v", facts)
	}

	paths.RootFS = filepath.Join(t.TempDir(), "missing-root")
	report, err = NewCollectorWithPaths(paths).Collect(time.Now().UTC())
	if err != nil || len(report.Unavailable) != 1 || report.Unavailable[0] != protocol.AreaDisk || report.Disk.TotalBytes != 0 {
		t.Fatalf("missing selected rootfs: unavailable=%v disk=%#v error=%v", report.Unavailable, report.Disk, err)
	}
}

func TestConfiguredSourcesDoNotFallBackWhenUnavailable(t *testing.T) {
	directory := t.TempDir()
	paths := SourcePaths{
		ProcRoot: filepath.Join(directory, "missing-proc"), SysRoot: filepath.Join(directory, "missing-sys"),
		RootFS: filepath.Join(directory, "missing-root"), OSRelease: filepath.Join(directory, "missing-os-release"),
	}
	report, err := NewCollectorWithPaths(paths).Collect(time.Now().UTC())
	if err != nil || len(report.Unavailable) != protocol.MaxUnavailableAreas {
		t.Fatalf("unavailable selected sources: areas=%v error=%v", report.Unavailable, err)
	}
	facts := CollectHostFactsWithPaths(paths)
	if facts.OS != "linux" || facts.Kernel != "" || facts.BootID != "" || facts.Virtualization != "" {
		t.Fatalf("missing host source fell back to runtime filesystem: %#v", facts)
	}
}

func TestCollectionSourceNativeDefaultsArePreserved(t *testing.T) {
	for _, collector := range []Collector{NewCollector(), NewCollectorWithPaths(SourcePaths{})} {
		source := collector.(*LinuxCollector).source
		if source.procRoot != "/proc" || source.sysRoot != "/sys" || source.rootPath != "/" || source.osReleasePath != "/etc/os-release" {
			t.Fatalf("native collection source paths changed: %#v", source)
		}
	}
	source := linuxSourceWithPaths(SourcePaths{ProcRoot: "/selected-proc"})
	if source.procRoot != "/selected-proc" || source.sysRoot != "/sys" || source.rootPath != "/" || source.osReleasePath != "/etc/os-release" {
		t.Fatalf("partial override replaced unspecified native defaults: %#v", source)
	}
}

func TestClientUsesConfiguredSourcesForHelloAndReports(t *testing.T) {
	paths := hostSourceFixture(t)
	result := make(chan error, 1)
	exchange := func(connection *websocket.Conn) error {
		if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		hello, err := protocol.NewEnvelope(protocol.TypeHello, "source-session", 1, protocol.ServerHello{
			NodeID: "source-node", ServerNonce: "nonce", HeartbeatIntervalSeconds: 5,
			HeartbeatTimeoutSeconds: 5, ReportIntervalMillis: protocol.MinReportInterval.Milliseconds(),
		})
		if err != nil {
			return err
		}
		if err := connection.WriteJSON(hello); err != nil {
			return err
		}
		var envelope protocol.Envelope
		if err := connection.ReadJSON(&envelope); err != nil {
			return err
		}
		var facts protocol.ClientHello
		if envelope.Type != protocol.TypeHello || envelope.Seq != 1 || protocol.DecodePayload(envelope.Payload, &facts) != nil ||
			facts.OS != "Fixture Host OS 42" || facts.Kernel != "fixture-kernel" || facts.BootID != sourceFixtureBootID || facts.Virtualization != "kvm" {
			return errors.New("Agent hello did not use configured host facts")
		}
		if err := connection.ReadJSON(&envelope); err != nil {
			return err
		}
		var report protocol.Report
		if envelope.Type != protocol.TypeReport || envelope.Seq != 2 || protocol.DecodePayload(envelope.Payload, &report) != nil ||
			report.UptimeSeconds != 123 || report.ProcessCount != 2 || report.Network.ReceiveBytesTotal != 100 || len(report.Unavailable) != 0 {
			return errors.New("Agent report did not use configured host sources")
		}
		if strings.Contains(string(envelope.Payload), sourceFixtureBootID) || strings.Contains(string(envelope.Payload), paths.ProcRoot) {
			return errors.New("Agent report leaked local source paths or boot identity")
		}
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(response, request, nil)
		if err != nil {
			result <- err
			return
		}
		defer connection.Close()
		result <- exchange(connection)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	client := &Client{Config: Config{ControlURL: server.URL, NodeID: "source-node", Credential: "source-credential"}, SourcePaths: paths}
	_ = client.ConnectOnce(ctx)
	if ctx.Err() != nil {
		t.Fatalf("configured-source client timed out: %v", ctx.Err())
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("Agent did not complete the configured-source session")
	}
}
