#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: scripts/release.sh VERSION COMMIT OUTPUT_DIR" >&2
  exit 2
fi

version=$1
commit=$2
output=$3

if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "VERSION must be an exact release such as v1.2.3" >&2
  exit 2
fi
case "$commit" in
  *[!0-9a-fA-F]*|'') echo "COMMIT must be a hexadecimal revision" >&2; exit 2 ;;
esac
if [ "${#commit}" -lt 7 ] || [ "${#commit}" -gt 64 ]; then
  echo "COMMIT must contain 7 to 64 hexadecimal characters" >&2
  exit 2
fi
if [ "${#version}" -gt 64 ]; then
  echo "VERSION is too long" >&2
  exit 2
fi

if [ -L "$output" ]; then
  echo "OUTPUT_DIR must not be a symbolic link" >&2
  exit 2
fi
mkdir -p "$output"
if find "$output" -mindepth 1 -maxdepth 1 ! -type f -print -quit | grep -q .; then
  echo "OUTPUT_DIR contains a non-file entry; refusing to leave stale release content" >&2
  exit 2
fi
find "$output" -mindepth 1 -maxdepth 1 -type f -delete

corepack pnpm@10.18.0 --dir web install --frozen-lockfile
corepack pnpm@10.18.0 --dir web build

ldflags="-s -w -buildid= -X github.com/senhao-xu/vps-status/internal/versioninfo.Version=$version -X github.com/senhao-xu/vps-status/internal/versioninfo.Commit=$commit"
export CGO_ENABLED=0
export GOOS=linux
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}

GOARCH=amd64 go build -trimpath -tags webui -ldflags "$ldflags" -o "$output/vps-status-control-linux-amd64" ./cmd/control
GOARCH=amd64 go build -trimpath -ldflags "$ldflags" -o "$output/vps-status-agent-linux-amd64" ./cmd/agent
GOARCH=arm64 go build -trimpath -ldflags "$ldflags" -o "$output/vps-status-agent-linux-arm64" ./cmd/agent

native_arch=$(go env GOARCH)
for binary in "$output"/vps-status-*; do
  case "$binary" in
    *-amd64) artifact_arch=amd64 ;;
    *-arm64) artifact_arch=arm64 ;;
    *) echo "unexpected release artifact: $binary" >&2; exit 1 ;;
  esac
  metadata=$(go version -m "$binary")
  printf '%s\n' "$metadata" | grep -F 'CGO_ENABLED=0' >/dev/null
  printf '%s\n' "$metadata" | grep -F 'GOOS=linux' >/dev/null
  printf '%s\n' "$metadata" | grep -F "GOARCH=$artifact_arch" >/dev/null
  strings "$binary" | grep -F -m 1 "$version" >/dev/null
  strings "$binary" | grep -F -m 1 "$commit" >/dev/null
  if [ "$artifact_arch" = "$native_arch" ]; then
    component=agent
    case "$binary" in *-control-*) component=control ;; esac
    expected="vps-status-$component version=$version commit=$commit protocol=v1"
    actual=$("$binary" --version)
    if [ "$actual" != "$expected" ]; then
      echo "release metadata mismatch for $binary" >&2
      exit 1
    fi
  fi
done

install -m 0755 internal/control/bootstrap-agent.sh "$output/bootstrap-agent.sh"
install -m 0755 deploy/systemd/install-agent.sh "$output/install-agent.sh"
install -m 0755 deploy/systemd/uninstall-agent.sh "$output/uninstall-agent.sh"
install -m 0644 deploy/systemd/vps-status-agent.service "$output/vps-status-agent.service"

(
  cd "$output"
  sha256sum \
    vps-status-control-linux-amd64 \
    vps-status-agent-linux-amd64 \
    vps-status-agent-linux-arm64 \
    bootstrap-agent.sh \
    install-agent.sh \
    uninstall-agent.sh \
    vps-status-agent.service > SHA256SUMS
)
