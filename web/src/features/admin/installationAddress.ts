import type { InstallationConfig } from '../../api/contracts'

export function isRemoteControlUrl(value: string): boolean {
  try {
    const url = new URL(value)
    const hostname = url.hostname.toLowerCase().replace(/\.$/, '')
    return value.length <= 2048 && url.protocol === 'https:' && !url.username && !url.password && !url.href.includes('?') && !url.hash &&
      hostname !== 'localhost' && !hostname.endsWith('.localhost') &&
      !/^127\./.test(hostname) && hostname !== '0.0.0.0' && hostname !== '[::]' && hostname !== '[::1]' &&
      !/^\[::ffff:7f[0-9a-f]{2}:/.test(hostname) && hostname !== '[::ffff:0:0]'
  } catch { return false }
}

export function resolveInstallationAddress(config: InstallationConfig, origin: string): string | null {
  const url = config.controlUrl || origin
  return isRemoteControlUrl(url) ? url : null
}

export function isAgentReleaseVersion(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(value) && !['dev', 'unknown'].includes(value.toLowerCase())
}
