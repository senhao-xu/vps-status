#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "installation requires root; the installed Agent service runs non-root" >&2
  exit 1
fi
if [ "$#" -ne 1 ] && [ "$#" -ne 3 ]; then
  echo "usage: install-agent.sh AGENT_BINARY [CONTROL_URL ENROLLMENT_TOKEN]" >&2
  exit 2
fi

binary=$1
test -f "$binary"
if [ ! -e /etc/vps-status/agent.json ] && [ ! -L /etc/vps-status/agent.json ] && [ "$#" -ne 3 ]; then
  echo "CONTROL_URL and ENROLLMENT_TOKEN are required for first installation" >&2
  exit 2
fi

if ! getent group vps-status-agent >/dev/null 2>&1; then
  groupadd --system vps-status-agent
fi
if ! id vps-status-agent >/dev/null 2>&1; then
  useradd --system --gid vps-status-agent --home-dir /nonexistent --shell /usr/sbin/nologin vps-status-agent
fi
if [ -L /etc/vps-status ] || { [ -e /etc/vps-status ] && [ ! -d /etc/vps-status ]; }; then
  echo "/etc/vps-status must be a real directory, not a symbolic link" >&2
  exit 1
fi
if [ -e /etc/vps-status/agent.json ] || [ -L /etc/vps-status/agent.json ]; then
  if [ -L /etc/vps-status/agent.json ] || [ ! -f /etc/vps-status/agent.json ] ||
     [ "$(stat -c '%U:%G:%a' /etc/vps-status/agent.json)" != "vps-status-agent:vps-status-agent:600" ]; then
    echo "existing Agent config must be a non-symlink mode-0600 file owned by vps-status-agent" >&2
    exit 1
  fi
fi

install -o root -g root -m 0755 "$binary" /usr/local/bin/vps-status-agent
install -d -o vps-status-agent -g vps-status-agent -m 0700 /etc/vps-status
install -o root -g root -m 0644 "$(dirname "$0")/vps-status-agent.service" /etc/systemd/system/vps-status-agent.service
if [ -e /etc/vps-status/agent.json ]; then
  echo "Existing Agent credential retained."
else
  control_url=$2
  token=$3
  runuser -u vps-status-agent -- /usr/local/bin/vps-status-agent enroll --control-url "$control_url" --token "$token" --config /etc/vps-status/agent.json
fi
systemctl daemon-reload
systemctl enable vps-status-agent.service
systemctl restart vps-status-agent.service
echo "VPS Status Agent installed as the unprivileged vps-status-agent user."
