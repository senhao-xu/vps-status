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

export function localInstallationAddress(value: string): string | null {
  try {
    const url = new URL(value)
    const hostname = url.hostname.toLowerCase()
    const loopback = hostname === 'localhost' || /^127(?:\.[0-9]{1,3}){3}$/u.test(hostname) || hostname === '[::1]' || /^\[::ffff:7f[0-9a-f]{2}:[0-9a-f]{1,4}\]$/u.test(hostname)
    if (value.length > 2048 || value.trim() !== value || !['http:', 'https:'].includes(url.protocol) ||
      url.username || url.password || url.href.includes('?') || url.href.includes('#') || !loopback) return null
    // URL normalizes numeric IPv4 aliases and IPv6 into literals Go can parse.
    return url.href.replace(/\/+$/u, '')
  } catch { return null }
}

export function resolveInstallationAddress(config: InstallationConfig, origin: string, localTest = false): string | null {
  if (config.controlUrl) return isRemoteControlUrl(config.controlUrl) ? config.controlUrl : null
  if (isRemoteControlUrl(origin)) return origin
  return localTest ? localInstallationAddress(origin) : null
}

export function isAgentReleaseVersion(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(value) && !['dev', 'unknown'].includes(value.toLowerCase())
}
