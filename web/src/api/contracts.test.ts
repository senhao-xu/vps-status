import { describe, expect, it } from 'vitest'

import { decodeEnrollment, decodeLiveEvent, decodePublicStatus, decodeReport, decodeStorageStats, reduceNodeEvent, type Node } from './contracts'

const publicNode = {
  name: 'Singapore edge', group: 'SG', online: true, uptime_seconds: 60, cpu_usage_percent: 10,
  memory_used_bytes: 1, memory_total_bytes: 2, disk_used_bytes: 3, disk_total_bytes: 4,
  receive_bytes_per_second: 5, transmit_bytes_per_second: 6, month_receive_bytes: 7, month_transmit_bytes: 8,
}

describe('decodePublicStatus', () => {
  it('decodes only the documented public projection', () => {
    const result = decodePublicStatus({ page: { enabled: true, title: 'Status', description: '' }, nodes: [publicNode] })
    expect(result.nodes[0]?.name).toBe('Singapore edge')
    expect(result.nodes[0]?.monthTransmitBytes).toBe(8)
  })

  it.each([
    'id', 'node_id', 'hostname', 'ip', 'os', 'kernel', 'architecture', 'virtualization',
    'credential', 'token', 'secret', 'probe_target', 'host', 'history', 'points', 'bucket_start', 'receive_bytes_total',
  ])(
    'rejects forbidden public field %s',
    (field) => {
      expect(() => decodePublicStatus({
        page: { enabled: true, title: 'Status', description: '' },
        nodes: [{ ...publicNode, [field]: 'private-sentinel' }],
      })).toThrow(/forbidden field/)
    },
  )

  it('rejects malformed and unexpected top-level data', () => {
    expect(() => decodePublicStatus({ page: { enabled: true, title: 'Status', description: '' }, nodes: [], admin: {} })).toThrow()
    expect(() => decodePublicStatus({ page: { enabled: 'yes', title: 'Status', description: '' }, nodes: [] })).toThrow()
  })
})

const report = {
  collected_at_unix_milli: Date.parse('2026-10-11T00:01:00Z'),
  cpu: { usage_percent: 0 },
  load: { one: 0, five: 0, fifteen: 0 },
  memory: { total_bytes: 0, used_bytes: 0, available_bytes: 0, swap_total_bytes: 0, swap_used_bytes: 0 },
  disk: { total_bytes: 0, used_bytes: 0, free_bytes: 0 },
  network: { receive_bytes_per_second: 0, transmit_bytes_per_second: 0, receive_bytes_total: 0, transmit_bytes_total: 0 },
  uptime_seconds: 0,
  process_count: 0,
  connections: { tcp: 0, udp: 0 },
  unavailable: ['cpu', 'memory', 'network'],
}

describe('decodeReport', () => {
  it('projects unavailable metric areas to null instead of measured zero', () => {
    const decoded = decodeReport(report)
    expect(decoded.cpuUsagePercent).toBeNull()
    expect(decoded.memoryTotalBytes).toBeNull()
    expect(decoded.receiveBytesPerSecond).toBeNull()
    expect(decoded.loadOne).toBe(0)
  })

  it('rejects unknown fields and unknown or duplicate unavailable areas', () => {
    expect(() => decodeReport({ ...report, private_fact: 'secret' })).toThrow(/forbidden field/)
    expect(() => decodeReport({ ...report, unavailable: ['cpu', 'cpu'] })).toThrow(/unknown or duplicate/)
    expect(() => decodeReport({ ...report, unavailable: ['temperature'] })).toThrow(/unknown or duplicate/)
  })
})

describe('decodeStorageStats', () => {
  it('keeps unavailable file statistics null instead of inventing measured zeroes', () => {
    const decoded = decodeStorageStats({
      state: {
        available: false, writable: false, reason: 'not_configured', queue_depth: 0, queue_capacity: 0,
        dropped: 0, updated_at: '2026-10-11T00:00:00Z',
      },
    })
    expect(decoded.mainBytes).toBeNull()
    expect(decoded.freeDiskBytes).toBeNull()
    expect(decoded.freelistPages).toBeNull()
  })
})

describe('decodeEnrollment', () => {
  it('keeps the one-command installer distinct from manual enrollment', () => {
    const decoded = decodeEnrollment({
      id: 'enrollment-1', enrollment_token: 'secret', expires_at: '2026-10-11T00:00:00Z',
      install_command: 'curl install-agent.sh | sudo sh', enroll_command: 'vps-status-agent enroll',
    })
    expect(decoded.installCommand).toContain('install-agent.sh')
    expect(decoded.enrollCommand).toContain('agent enroll')
  })

  it('requires both installation modes and rejects extras', () => {
    const base = {
      id: 'enrollment-1', enrollment_token: 'secret', expires_at: '2026-10-11T00:00:00Z',
      install_command: 'install', enroll_command: 'enroll',
    }
    expect(() => decodeEnrollment({
      id: base.id, enrollment_token: base.enrollment_token, expires_at: base.expires_at, install_command: base.install_command,
    })).toThrow()
    expect(() => decodeEnrollment({ ...base, credential: 'private' })).toThrow(/forbidden field/)
  })
})

describe('decodeLiveEvent', () => {
  it('requires a report only for report events', () => {
    const base = { node_id: 'node-1', connected: true, online: true, received_at: '2026-10-11T00:01:00Z' }
    expect(() => decodeLiveEvent({ ...base, kind: 'report' })).toThrow(/missing its report/)
    expect(() => decodeLiveEvent({ ...base, kind: 'connected', report })).toThrow(/contains a report/)
    expect(decodeLiveEvent({ ...base, kind: 'report', report }).report).not.toBeNull()
  })
})

describe('reduceNodeEvent', () => {
  const node: Node = {
    id: 'node-1', name: 'Edge', group: '', hidden: false, hostname: '', os: '', kernel: '', architecture: '', virtualization: '',
    connected: true, online: true, lastReportAt: '2026-10-11T00:01:00Z', report: null,
  }

  it('does not let an older report replace newer live state', () => {
    const result = reduceNodeEvent([node], {
      kind: 'report', nodeId: 'node-1', connected: true, online: true, receivedAt: '2026-10-11T00:00:00Z', report: null,
    })
    expect(result[0]?.lastReportAt).toBe('2026-10-11T00:01:00Z')
  })

  it('keeps last report time when a node disconnects', () => {
    const result = reduceNodeEvent([node], {
      kind: 'disconnected', nodeId: 'node-1', connected: false, online: false, receivedAt: '2026-10-11T00:02:00Z', report: null,
    })
    expect(result[0]?.online).toBe(false)
    expect(result[0]?.lastReportAt).toBe('2026-10-11T00:01:00Z')
  })

  it.each(['connected', 'stale', 'disconnected'] as const)('ignores an older %s event', (kind) => {
    const result = reduceNodeEvent([node], {
      kind, nodeId: 'node-1', connected: kind !== 'disconnected', online: false,
      receivedAt: '2026-10-11T00:00:00Z', report: null,
    })
    expect(result[0]).toEqual(node)
  })
})
