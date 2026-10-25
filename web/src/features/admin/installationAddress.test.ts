import { describe, expect, it } from 'vitest'

import type { InstallationConfig } from '../../api/contracts'
import { isAgentReleaseVersion, isRemoteControlUrl, localInstallationAddress, resolveInstallationAddress } from './installationAddress'

const config: InstallationConfig = {
  controlUrl: '', agentReleaseVersion: '', effectiveAgentReleaseVersion: 'v1.0.0',
  agentReleaseBaseUrl: 'https://github.com/senhao-xu/vps-status/releases/download', releaseConfigured: true,
}

describe('remote enrollment address', () => {
  it('prioritizes the saved address even when the administrator visits localhost', () => {
    expect(resolveInstallationAddress({ ...config, controlUrl: 'https://status.example.com/panel' }, 'http://localhost:8080')).toBe('https://status.example.com/panel')
  })

  it('uses an HTTPS page origin when the saved address is empty', () => {
    expect(resolveInstallationAddress(config, 'https://status.example.com')).toBe('https://status.example.com')
  })

  it.each(['http://localhost:8080', 'https://localhost', 'https://local.localhost', 'https://localhost.', 'https://127.0.0.1', 'https://127.22.33.44', 'https://127.1', 'https://2130706433', 'https://[::1]', 'https://[0:0:0:0:0:0:0:1]', 'https://[::ffff:127.0.0.1]', 'https://0.0.0.0', 'https://[::]', 'http://status.example.com', 'https://user:password@status.example.com', 'https://status.example.com?token=secret', 'https://status.example.com?', 'https://status.example.com#fragment', 'not-an-address'])('blocks unsafe automatic command address %s', (origin) => {
    expect(resolveInstallationAddress(config, origin)).toBeNull()
  })

  it.each(['https://status.example.com', 'https://192.168.1.10:8443', 'https://[2001:db8::1]', 'https://status.example.com/panel/'])('supports remotely reachable and private HTTPS setups %s', (url) => {
    expect(isRemoteControlUrl(url)).toBe(true)
  })

  it('does not fall back silently when a saved address is invalid', () => {
    expect(resolveInstallationAddress({ ...config, controlUrl: 'https://localhost' }, 'https://status.example.com')).toBeNull()
  })
})

describe('explicit local enrollment address', () => {
  it.each([
    ['http://localhost:8080', 'http://localhost:8080'],
    ['https://localhost:8443/', 'https://localhost:8443'],
    ['http://LOCALHOST:8080', 'http://localhost:8080'],
    ['http://127.0.0.1:8080', 'http://127.0.0.1:8080'],
    ['https://127.22.33.44:8443', 'https://127.22.33.44:8443'],
    ['http://127.1:8080', 'http://127.0.0.1:8080'],
    ['http://2130706433:8080', 'http://127.0.0.1:8080'],
    ['http://0x7f000001:8080', 'http://127.0.0.1:8080'],
    ['http://[::1]:8080', 'http://[::1]:8080'],
    ['https://[0:0:0:0:0:0:0:1]:8443/', 'https://[::1]:8443'],
    ['http://[::ffff:127.0.0.1]:8080', 'http://[::ffff:7f00:1]:8080'],
    ['http://[0:0:0:0:0:ffff:7fff:ffff]:8080', 'http://[::ffff:7fff:ffff]:8080'],
  ])('requires explicit opt-in and serializes %s as %s', (origin, canonical) => {
    expect(localInstallationAddress(origin)).toBe(canonical)
    expect(resolveInstallationAddress(config, origin)).toBeNull()
    expect(resolveInstallationAddress(config, origin, true)).toBe(canonical)
    expect(resolveInstallationAddress(config, origin, false)).toBeNull()
  })

  it.each([
    '', 'not-an-address', 'ftp://localhost', 'ws://localhost:8080',
    'http://status.example.com', 'http://192.168.1.10:8080', 'http://127.example.com',
    'http://127.0.0.1.example.com', 'http://0.0.0.0', 'http://0', 'http://[::]',
    'http://[2001:db8::1]', 'http://[::ffff:192.168.1.10]', 'http://[::127.0.0.1]',
    'http://local.localhost', 'https://local.localhost', 'http://localhost.', 'https://localhost.',
    'http://user:password@localhost', 'http://user@127.0.0.1',
    'http://localhost?token=secret', 'http://localhost?', 'http://localhost#fragment', 'http://localhost#',
    ' http://localhost', 'http://localhost ', `http://localhost/${'x'.repeat(2048)}`,
  ])('rejects %j as a local target', (origin) => {
    expect(localInstallationAddress(origin)).toBeNull()
    // Only the separate automatic remote-HTTPS branch may accept remote origins.
    if (!isRemoteControlUrl(origin)) expect(resolveInstallationAddress(config, origin, true)).toBeNull()
  })

  it('prioritizes saved remote settings and never falls back from invalid saved settings', () => {
    const origin = 'http://localhost:8080'
    expect(resolveInstallationAddress({ ...config, controlUrl: 'https://status.example.com/panel' }, origin, true)).toBe('https://status.example.com/panel')
    for (const controlUrl of ['http://localhost:8080', 'https://localhost', 'http://status.example.com', 'not-an-address']) {
      expect(resolveInstallationAddress({ ...config, controlUrl }, origin, true)).toBeNull()
      expect(resolveInstallationAddress({ ...config, controlUrl }, 'https://status.example.com', true)).toBeNull()
    }
  })

  it('retains automatic remote HTTPS resolution regardless of the local choice', () => {
    const origin = 'https://status.example.com'
    expect(localInstallationAddress(origin)).toBeNull()
    expect(resolveInstallationAddress(config, origin, true)).toBe(origin)
    expect(resolveInstallationAddress(config, origin, false)).toBe(origin)
  })
})

describe('release version input', () => {
  it.each(['v1.0.0', '1.2.3-beta_1', 'a'.repeat(64)])('accepts a bounded published version identifier %s', (value) => {
    expect(isAgentReleaseVersion(value)).toBe(true)
  })
  it.each(['', 'dev', 'DEV', 'unknown', 'Unknown', '../v1', 'v1/asset', 'v 1', 'v1?token', 'a'.repeat(65)])('rejects placeholders or unsafe path segments %s', (value) => {
    expect(isAgentReleaseVersion(value)).toBe(false)
  })
})
