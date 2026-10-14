package control

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstallScriptEndpointIsPublicAndPinned(t *testing.T) {
	handler, _ := newTestAdminAPI(t, func(config *APIConfig) {
		config.AgentReleaseBaseURL = "https://releases.example/vps-status"
		config.AgentReleaseVersion = "v1.2.3"
	})
	request := httptest.NewRequest(http.MethodGet, agentInstallScriptPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installer status = %d; want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/x-shellscript; charset=utf-8" {
		t.Fatalf("installer Content-Type = %q", contentType)
	}
	script := response.Body.String()
	for _, expected := range []string{
		"default_release_base='https://releases.example/vps-status'",
		"default_release_version='v1.2.3'",
		"x86_64|amd64) agent_arch=amd64",
		"aarch64|arm64) agent_arch=arm64",
		"download SHA256SUMS",
		"verify_file \"$agent_file\"",
		"verify_file install-agent.sh",
		"verify_file vps-status-agent.service",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("installer script does not contain %q", expected)
		}
	}
	command := exec.Command("sh", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("installer shell syntax: %v: %s", err, output)
	}
}

func TestAgentReleaseConfigurationRejectsCommandInjection(t *testing.T) {
	for _, test := range []struct {
		name    string
		baseURL string
		version string
	}{
		{name: "plaintext source", baseURL: "http://releases.example/vps-status", version: "v1.2.3"},
		{name: "source query", baseURL: "https://releases.example/vps-status?file=x", version: "v1.2.3"},
		{name: "version shell syntax", baseURL: "https://releases.example/vps-status", version: "v1.2.3;id"},
		{name: "version path", baseURL: "https://releases.example/vps-status", version: "../v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizeAgentReleaseConfig(test.baseURL, test.version); err == nil {
				t.Fatal("unsafe Agent release configuration was accepted")
			}
		})
	}
}

// Mock all host-facing tools. The downloaded installer only records invocation;
// these tests never enroll a real node or write service/configuration directories.
func TestBootstrapExecutableDownloadsAndChecksums(t *testing.T) {
	for _, scenario := range []string{"success-amd64", "success-arm64", "success-path-at", "mismatch-agent", "mismatch-installer", "mismatch-service", "missing", "duplicate", "malformed", "download-failure", "unsupported-arch", "unsupported-os", "not-root", "dev-version", "invalid-base", "base-credentials", "base-without-host"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			fixtures := filepath.Join(directory, "release")
			commands := filepath.Join(directory, "bin")
			temporary := filepath.Join(directory, "tmp")
			for _, path := range []string{fixtures, commands, temporary} {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(directory, "installed")
			downloadLog := filepath.Join(directory, "downloads")
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				"vps-status-agent-linux-amd64": "test amd64 Agent binary",
				"vps-status-agent-linux-arm64": "test arm64 Agent binary",
				"vps-status-agent.service":     "User=vps-status-agent\n",
				"install-agent.sh":             "#!/bin/sh\nset -eu\ntest -f \"$1\"\ntest \"$2\" = \"$EXPECTED_CONTROL\"\ntest \"$3\" = \"$EXPECTED_TOKEN\"\nprintf 'installed\\n' > \"$INSTALL_MARKER\"\n",
			}
			var sums strings.Builder
			for name, content := range files {
				write(filepath.Join(fixtures, name), content)
				digest := sha256.Sum256([]byte(content))
				fmt.Fprintf(&sums, "%x  %s\n", digest, name)
			}
			checksumFile := sums.String()
			agentSum := fmt.Sprintf("%x  vps-status-agent-linux-amd64", sha256.Sum256([]byte(files["vps-status-agent-linux-amd64"])))
			switch scenario {
			case "mismatch-agent":
				write(filepath.Join(fixtures, "vps-status-agent-linux-amd64"), "tampered binary")
			case "mismatch-installer":
				write(filepath.Join(fixtures, "install-agent.sh"), "#!/bin/sh\nprintf compromised > \"$INSTALL_MARKER\"\n")
			case "mismatch-service":
				write(filepath.Join(fixtures, "vps-status-agent.service"), "User=root\n")
			case "missing":
				checksumFile = strings.ReplaceAll(checksumFile, agentSum+"\n", "")
			case "duplicate":
				checksumFile += agentSum + "\n"
			case "malformed":
				checksumFile = strings.ReplaceAll(checksumFile, agentSum, "invalid  vps-status-agent-linux-amd64")
			}
			write(filepath.Join(fixtures, "SHA256SUMS"), checksumFile)
			write(filepath.Join(commands, "curl"), `#!/bin/sh
set -eu
output=
url=
while [ "$#" -gt 0 ]; do
 case "$1" in
  --output) output=$2; shift 2 ;;
  --proto|--proto-redir) test "$2" = '=https'; shift 2 ;;
  --tlsv1.2) shift ;;
  --fail|--silent|--show-error|--location) shift ;;
  https://*) url=$1; shift ;;
  *) exit 90 ;;
 esac
done
test -n "$output"
case "$url" in https://releases.example/v1.2.3/*|https://releases.example/@vps/v1.2.3/*) ;; *) exit 91 ;; esac
printf '%s\n' "$url" >> "$DOWNLOAD_LOG"
if [ "$SCENARIO" = download-failure ]; then exit 22; fi
cp "$FIXTURES/${url##*/}" "$output"
`)
			write(filepath.Join(commands, "id"), "#!/bin/sh\nif [ \"$SCENARIO\" = not-root ]; then echo 1000; else echo 0; fi\n")
			write(filepath.Join(commands, "uname"), `#!/bin/sh
case "$1" in
 -s) if [ "$SCENARIO" = unsupported-os ]; then echo Darwin; else echo Linux; fi ;;
 -m) case "$SCENARIO" in success-arm64) echo aarch64 ;; unsupported-arch) echo riscv64 ;; *) echo x86_64 ;; esac ;;
esac
`)
			write(filepath.Join(commands, "systemctl"), "#!/bin/sh\nexit 98\n") // Unexpected execution must fail.
			script := filepath.Join(directory, "bootstrap.sh")
			write(script, agentBootstrapScript)
			control, token := "https://control.example/panel", "one-time-secret-test"
			version, base := "v1.2.3", "https://releases.example"
			if scenario == "dev-version" {
				version = "DeV"
			}
			if scenario == "invalid-base" {
				base = "http://releases.example"
			}
			if scenario == "success-path-at" {
				base = "https://releases.example/@vps"
				if _, _, err := normalizeAgentReleaseConfig(base, version); err != nil {
					t.Fatalf("valid mirror path rejected: %v", err)
				}
			}
			if scenario == "base-credentials" {
				base = "https://user:password@releases.example"
			}
			if scenario == "base-without-host" {
				base = "https:///releases.example"
			}
			command := exec.Command("sh", script, control, token, version, base)
			command.Env = append(os.Environ(), "PATH="+commands+":"+os.Getenv("PATH"), "TMPDIR="+temporary, "FIXTURES="+fixtures, "SCENARIO="+scenario, "INSTALL_MARKER="+marker, "EXPECTED_CONTROL="+control, "EXPECTED_TOKEN="+token, "DOWNLOAD_LOG="+downloadLog)
			output, err := command.CombinedOutput()
			success := strings.HasPrefix(scenario, "success-")
			if success && err != nil || !success && err == nil {
				t.Fatalf("execution err=%v output=%s", err, output)
			}
			_, installedErr := os.Stat(marker)
			if success && installedErr != nil || !success && !os.IsNotExist(installedErr) {
				t.Fatalf("unexpected installer invocation: %v output=%s", installedErr, output)
			}
			if strings.Contains(string(output), token) {
				t.Fatal("bootstrap leaked token")
			}
			leftovers, err := os.ReadDir(temporary)
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files not cleaned: %v %v", leftovers, err)
			}
			if success {
				downloads, err := os.ReadFile(downloadLog)
				if err != nil {
					t.Fatal(err)
				}
				if len(strings.Fields(string(downloads))) != 4 {
					t.Fatalf("unexpected downloads: %s", downloads)
				}
				arch := "amd64"
				if scenario == "success-arm64" {
					arch = "arm64"
				}
				if !strings.Contains(string(downloads), "vps-status-agent-linux-"+arch) {
					t.Fatalf("wrong architecture: %s", downloads)
				}
			}
		})
	}
}

func TestInstallCommandQuotesAllArguments(t *testing.T) {
	directory := t.TempDir()
	argFile := filepath.Join(directory, "args")
	urlFile := filepath.Join(directory, "url")
	control, token := "https://control.example/path'with-quote", "token'$(echo unsafe)"
	base, version := "https://releases.example/path'quoted", "v1.2.3"
	command := agentInstallCommand(control, token, base, version)
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write("curl", "#!/bin/sh\nfor value do url=$value; done\nprintf '%s' \"$url\" > \"$URL_OUTPUT\"\nprintf '%s\\n' '#!/bin/sh' 'printf \"%s\\n\" \"$@\" > \"$ARG_OUTPUT\"'\n")
	write("sudo", "#!/bin/sh\nexec \"$@\"\n")
	check := exec.Command("sh", "-c", command)
	check.Env = append(os.Environ(), "PATH="+directory+":"+os.Getenv("PATH"), "ARG_OUTPUT="+argFile, "URL_OUTPUT="+urlFile)
	if output, err := check.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("command execution=%v %s", err, output)
	}
	args, err := os.ReadFile(argFile)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Join([]string{control, token, version, base}, "\n") + "\n"
	if string(args) != expected {
		t.Fatalf("arguments changed by shell: %q want %q", args, expected)
	}
	downloadedURL, err := os.ReadFile(urlFile)
	if err != nil || string(downloadedURL) != base+"/"+version+"/bootstrap-agent.sh" {
		t.Fatalf("download URL=%q err=%v", downloadedURL, err)
	}
}
