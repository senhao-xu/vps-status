#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "uninstallation requires root" >&2
  exit 1
fi
if [ "$#" -gt 1 ] || { [ "$#" -eq 1 ] && [ "$1" != "--purge-config" ]; }; then
  echo "usage: uninstall-agent.sh [--purge-config]" >&2
  exit 2
fi

systemctl disable --now vps-status-agent.service 2>/dev/null || true
rm -f /etc/systemd/system/vps-status-agent.service /usr/local/bin/vps-status-agent
systemctl daemon-reload
if [ "${1:-}" = "--purge-config" ]; then
  rm -f /etc/vps-status/agent.json
  if rmdir /etc/vps-status 2>/dev/null; then
    userdel vps-status-agent 2>/dev/null || true
    groupdel vps-status-agent 2>/dev/null || true
    echo "Agent, credential config, and service account removed."
  else
    echo "Agent and credential config removed; additional /etc/vps-status content and the service account were retained."
  fi
else
  echo "Agent removed; /etc/vps-status/agent.json and the service account were retained."
fi
