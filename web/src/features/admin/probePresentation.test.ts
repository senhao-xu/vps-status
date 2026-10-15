import { describe, expect, it } from 'vitest'

import type { Node, ProbeLatest, ProbeTask } from '../../api/contracts'
import { latestProbeResult, matchesProbe, probeHistoryQueries, probeSummary, probeTarget } from './probePresentation'

const result = (nodeId: string, success: boolean, minute = 0): ProbeLatest => ({
  nodeId, success, receivedAt: `2026-10-14T08:${String(minute).padStart(2, '0')}:00Z`, latencyMillis: success ? 12 : 0, error: success ? '' : 'timeout',
})
const task: ProbeTask = { id: 'task', name: 'HTTPS gateway', host: 'example.com', port: 443, intervalSeconds: 30, autoJoin: false, updatedAt: '2026-10-14T07:00:00Z', nodeIds: ['a', 'b'], latest: [] }
const node = (id: string, name: string, group: string): Node => ({
  id, name, group, hidden: false, homepageVisible: false, hostname: '', os: '', kernel: '', architecture: '', virtualization: '',
  connected: false, online: false, lastReportAt: null, report: null,
})

describe('TCP presentation', () => {
  it('counts each assigned node/task once, ignores detached results and distinguishes missing samples from success', () => {
    const probes = [
      { ...task, latest: [result('a', false, 1), result('removed', false), result('a', true, 2)] },
      { ...task, id: 'second', nodeIds: ['a'], latest: [result('a', false)] },
      { ...task, id: 'unassigned', nodeIds: [], latest: [result('removed', true)] },
    ]
    expect(probeSummary(probes)).toEqual({ tasks: 3, nodes: 2, failures: 1, waiting: 1 })
    expect(latestProbeResult(probes[0], 'a')?.success).toBe(true)
    expect(latestProbeResult(probes[0], 'b')).toBeUndefined()
    expect(latestProbeResult(probes[0], 'removed')).toBeUndefined()
  })

  it('compares sample timestamps chronologically when fractional precision or timezone offsets differ', () => {
    const earlier = { ...result('a', false), receivedAt: '2026-10-14T08:00:00Z' }
    const newer = { ...result('a', true), receivedAt: '2026-10-14T08:00:00.100Z' }
    expect(latestProbeResult({ ...task, latest: [earlier, newer] }, 'a')).toBe(newer)
    const offset = { ...result('a', false), receivedAt: '2026-10-14T07:01:00-01:00' }
    expect(latestProbeResult({ ...task, latest: [newer, offset] }, 'a')).toBe(offset)
  })

  it('matches a task by target or assigned node/group, and excludes unrelated nodes', () => {
    const nodes = [node('a', 'Tokyo', 'Asia'), node('other', 'Paris', 'Europe')]
    expect(matchesProbe(task, nodes, '  tOkYo ')).toBe(true)
    expect(matchesProbe(task, nodes, 'Asia')).toBe(true)
    expect(matchesProbe(task, nodes, 'EXAMPLE.COM:443')).toBe(true)
    expect(matchesProbe(task, nodes, 'gateway')).toBe(true)
    expect(matchesProbe(task, nodes, 'Paris')).toBe(false)
    expect(matchesProbe(task, nodes, 'Europe')).toBe(false)
  })

  it('shows IPv6 endpoints without ambiguity', () => {
    expect(probeTarget({ host: '::1', port: 443 })).toBe('[::1]:443')
    expect(probeTarget({ host: '127.0.0.1', port: 22 })).toBe('127.0.0.1:22')
  })

  it('ignores old-target results after an edit until this configuration has a fresh sample', () => {
    const edited = { ...task, updatedAt: '2026-10-14T08:02:00Z', latest: [result('a', true, 1), result('b', false, 1)] }
    expect(latestProbeResult(edited, 'a')).toBeUndefined()
    expect(probeSummary([edited])).toEqual({ tasks: 1, nodes: 2, failures: 0, waiting: 2 })
    const fresh = result('a', false, 3)
    expect(latestProbeResult({ ...edited, latest: [...edited.latest, fresh] }, 'a')).toBe(fresh)
  })

  it('waits through the ambiguous configuration-update second and accepts the next-second sample', () => {
    const edited = { ...task, updatedAt: '2026-10-14T08:02:00Z' }
    const sameSecond = { ...result('a', true, 2), receivedAt: '2026-10-14T08:02:00.999Z' }
    const exactUpdate = result('a', true, 2)
    const waiting = { ...edited, latest: [exactUpdate, sameSecond] }
    expect(latestProbeResult(waiting, 'a')).toBeUndefined()
    expect(probeSummary([waiting])).toEqual({ tasks: 1, nodes: 2, failures: 0, waiting: 2 })
    const nextSecond = { ...result('a', false, 2), receivedAt: '2026-10-14T08:02:01.000Z' }
    expect(latestProbeResult({ ...waiting, latest: [...waiting.latest, nextSecond] }, 'a')).toBe(nextSecond)
  })

  it.each([
    ['24h', [['1m', '2026-10-13T08:03:12.123Z', '2026-10-14T08:03:12.123Z']]],
    ['7d', [['10m', '2026-10-07T08:03:12.123Z', '2026-10-13T08:10:00.000Z'], ['1m', '2026-10-13T08:10:00.000Z', '2026-10-14T08:03:12.123Z']]],
    ['30d', [['1h', '2026-09-14T08:03:12.123Z', '2026-10-07T09:00:00.000Z'], ['10m', '2026-10-07T09:00:00.000Z', '2026-10-13T08:10:00.000Z'], ['1m', '2026-10-13T08:10:00.000Z', '2026-10-14T08:03:12.123Z']]],
  ] as const)('covers the whole %s range using adjacent half-open windows and explicit IDs', (range, expected) => {
    const queries = probeHistoryQueries('task/&', 'node=雪', range, new Date('2026-10-14T08:03:12.123Z')).map((query) => new URLSearchParams(query))
    expect(queries.map((query) => [query.get('resolution'), query.get('from'), query.get('to')])).toEqual(expected)
    for (const [index, query] of queries.entries()) {
      expect(query.get('task_id')).toBe('task/&')
      expect(query.get('node_id')).toBe('node=雪')
      if (index > 0) expect(queries[index - 1].get('to')).toBe(query.get('from'))
    }
  })

  it('does not advance transitions already exactly aligned to coarser buckets', () => {
    const queries = probeHistoryQueries('task', 'node', '30d', new Date('2026-10-14T08:00:00Z')).map((query) => new URLSearchParams(query))
    expect(queries.map((query) => [query.get('from'), query.get('to')])).toEqual([
      ['2026-09-14T08:00:00.000Z', '2026-10-07T08:00:00.000Z'],
      ['2026-10-07T08:00:00.000Z', '2026-10-13T08:00:00.000Z'],
      ['2026-10-13T08:00:00.000Z', '2026-10-14T08:00:00.000Z'],
    ])
  })
})
