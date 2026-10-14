import { describe, expect, it } from 'vitest'

import type { InstallationConfig } from '../../api/contracts'
import { isAgentReleaseVersion, isRemoteControlUrl, resolveInstallationAddress } from './installationAddress'

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

describe('release version input', () => {
  it.each(['v1.0.0', '1.2.3-beta_1', 'a'.repeat(64)])('accepts a bounded published version identifier %s', (value) => {
    expect(isAgentReleaseVersion(value)).toBe(true)
  })
  it.each(['', 'dev', 'DEV', 'unknown', 'Unknown', '../v1', 'v1/asset', 'v 1', 'v1?token', 'a'.repeat(65)])('rejects placeholders or unsafe path segments %s', (value) => {
    expect(isAgentReleaseVersion(value)).toBe(false)
  })
})
