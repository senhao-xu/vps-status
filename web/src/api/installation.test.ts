import { describe, expect, it } from 'vitest'

import { decodeInstallationConfig } from './contracts'

const config = {
  control_url: 'https://status.example.com', agent_release_version: '',
  effective_agent_release_version: 'v1.0.0',
  agent_release_base_url: 'https://github.com/senhao-xu/vps-status/releases/download',
  release_configured: true,
}

describe('installation configuration contract', () => {
  it('decodes persisted overrides separately from effective defaults', () => {
    expect(decodeInstallationConfig(config)).toEqual({
      controlUrl: config.control_url, agentReleaseVersion: '', effectiveAgentReleaseVersion: 'v1.0.0',
      agentReleaseBaseUrl: config.agent_release_base_url, releaseConfigured: true,
    })
    expect(decodeInstallationConfig({ ...config, control_url: '', effective_agent_release_version: 'dev', release_configured: false })).toMatchObject({ controlUrl: '', effectiveAgentReleaseVersion: 'dev', releaseConfigured: false })
  })

  it.each(Object.keys(config))('requires field %s', (field) => {
    const value: Record<string, unknown> = { ...config }
    delete value[field]
    expect(() => decodeInstallationConfig(value)).toThrow()
  })

  it.each([null, [], {}, { ...config, release_configured: 'true' }, { ...config, agent_release_version: null }, { ...config, enrollment_token: 'secret' }])('rejects malformed or excessive payloads', (value) => {
    expect(() => decodeInstallationConfig(value)).toThrow()
  })
})
