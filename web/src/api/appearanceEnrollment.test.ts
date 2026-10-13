import { describe, expect, it } from 'vitest'

import { decodeAppearance, decodeEnrollment, decodeEnrollmentStatus } from './contracts'

const base = { id: 'enrollment-1', node_name: 'Edge', expires_at: '2026-10-13T12:00:00Z' }

describe('appearance and enrollment status contracts', () => {
  it('retains the enrollment ID from the once-only response', () => {
    expect(decodeEnrollment({ id: base.id, expires_at: base.expires_at, enrollment_token: 'once-only-token', install_command: 'install', enroll_command: 'enroll' }).id).toBe(base.id)
  })

  it.each(['emerald', 'classic'])('accepts built-in %s', (value) => {
    expect(decodeAppearance({ default_theme: value })).toEqual({ defaultTheme: value })
  })

  it.each([{}, { default_theme: 'external-script' }, { default_theme: 'emerald', token: 'secret' }])('rejects malformed or excessive appearance data', (value) => {
    expect(() => decodeAppearance(value)).toThrow()
  })

  it.each(['pending', 'expired'])('accepts unconsumed %s without a node identity', (status) => {
    expect(decodeEnrollmentStatus({ ...base, status })).toEqual({ id: base.id, nodeName: base.node_name, expiresAt: base.expires_at, status })
    expect(() => decodeEnrollmentStatus({ ...base, status, node_id: 'node-1' })).toThrow()
  })

  it.each(['registered', 'online'])('requires an explicit node ID for %s', (status) => {
    expect(decodeEnrollmentStatus({ ...base, status, node_id: 'node-1' })).toMatchObject({ status, nodeId: 'node-1' })
    expect(() => decodeEnrollmentStatus({ ...base, status })).toThrow()
  })

  it('rejects unknown states, malformed expiry, and returned secrets', () => {
    expect(() => decodeEnrollmentStatus({ ...base, status: 'revoked' })).toThrow()
    expect(() => decodeEnrollmentStatus({ ...base, status: 'pending', expires_at: 'yesterday' })).toThrow()
    for (const key of ['enrollment_token', 'digest', 'credential']) {
      expect(() => decodeEnrollmentStatus({ ...base, status: 'pending', [key]: 'secret' })).toThrow()
    }
  })
})
