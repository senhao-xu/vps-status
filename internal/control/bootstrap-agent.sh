#!/bin/sh
set -eu
umask 077

# The compatibility endpoint injects defaults; the standalone Release asset
# requires an explicit version and release base supplied by the admin command.
if [ "$#" -eq 4 ]; then
  release_version=$3
  release_base=$4
elif [ "$#" -eq 2 ] && [ -n "${default_release_version:-}" ] && [ -n "${default_release_base:-}" ]; then
  release_version=$default_release_version
  release_base=$default_release_base
else
  echo "usage: bootstrap-agent.sh CONTROL_URL ENROLLMENT_TOKEN VERSION HTTPS_RELEASE_BASE" >&2
  exit 2
fi
case "$release_version" in
  [dD][eE][vV]|[uU][nN][kK][nN][oO][wW][nN]|''|*[!A-Za-z0-9._-]*|[!A-Za-z0-9]*)
    echo "a published Agent release version is required" >&2; exit 2 ;;
esac
if [ "${#release_version}" -gt 64 ]; then
  echo "release version is too long" >&2; exit 2
fi
case "$release_base" in
  https://?*) ;;
  *) echo "release base must use HTTPS" >&2; exit 2 ;;
esac
case "$release_base" in
  *'?'*|*'#'*|*' '*|*'\'*|*'
'*) echo "release base contains unsupported characters" >&2; exit 2 ;;
esac
release_authority=${release_base#https://}
release_authority=${release_authority%%/*}
case "$release_authority" in
  ''|*'@'*) echo "release base requires a host without credentials" >&2; exit 2 ;;
esac
if [ "$(id -u)" -ne 0 ]; then
  echo "installation requires root; the installed Agent service runs non-root" >&2
  exit 1
fi
if [ "$(uname -s)" != "Linux" ]; then
  echo "only Linux is supported" >&2
  exit 1
fi
case "$(uname -m)" in
  x86_64|amd64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "unsupported architecture; expected amd64 or arm64" >&2; exit 1 ;;
esac
for required_command in curl sha256sum awk systemctl; do
  if ! command -v "$required_command" >/dev/null 2>&1; then
    echo "required command is unavailable: $required_command" >&2
    exit 1
  fi
done

control_url=$1
enrollment_token=$2
agent_file="vps-status-agent-linux-$agent_arch"
release_url="${release_base%/}/$release_version"
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/vps-status-agent.XXXXXX")
cleanup() {
  rm -rf "$temporary_directory"
}
trap cleanup EXIT HUP INT TERM

download() {
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --output "$temporary_directory/$1" "$release_url/$1"
}
verify_file() {
  file=$1
  if ! checksum=$(awk -v wanted="$file" '$2 == wanted { if (seen) exit 2; value=$1; seen=1 } END { if (!seen) exit 1; print value }' "$temporary_directory/SHA256SUMS"); then
    echo "release checksum is missing or duplicated for $file" >&2
    exit 1
  fi
  if [ "${#checksum}" -ne 64 ]; then
    echo "release checksum is invalid for $file" >&2
    exit 1
  fi
  case "$checksum" in
    *[!0-9a-fA-F]*) echo "release checksum is invalid for $file" >&2; exit 1 ;;
  esac
  printf '%s  %s\n' "$checksum" "$temporary_directory/$file" | sha256sum -c -
}

download SHA256SUMS
download "$agent_file"
download install-agent.sh
download vps-status-agent.service
verify_file "$agent_file"
verify_file install-agent.sh
verify_file vps-status-agent.service
chmod 0700 "$temporary_directory/install-agent.sh"
"$temporary_directory/install-agent.sh" "$temporary_directory/$agent_file" "$control_url" "$enrollment_token"
unset enrollment_token
