import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIError, apiRequest } from '../../api/client'
import type { ProbePoint } from '../../api/contracts'
import { loadProbeHistory } from './probeHistory'

vi.mock('../../api/client', async (original) => ({ ...await original<typeof import('../../api/client')>(), apiRequest: vi.fn() }))
afterEach(() => { vi.clearAllMocks(); vi.useRealTimers() })

const point = (bucketStart: string, failures = 0): ProbePoint => ({ bucketStart, attempts: 1, failures, successRate: failures ? 0 : 1, latencyAverage: failures ? null : 12 })

describe('TCP mixed-resolution history', () => {
  it('fetches all retained tiers with one cancellation signal and merges recent plus older samples chronologically', async () => {
    vi.useFakeTimers(); vi.setSystemTime(new Date('2026-10-14T08:00:00Z'))
    const current = point('2026-10-14T07:59:00Z', 1), week = point('2026-10-10T08:00:00Z'), month = point('2026-09-25T08:00:00Z')
    vi.mocked(apiRequest).mockImplementation(async (path) => {
      const query = new URLSearchParams(path.split('?')[1])
      switch (query.get('resolution')) {
        case '1m': return [current]
        case '10m': return [week]
        case '1h': return [month]
        default: throw new Error('unexpected resolution')
      }
    })
    const controller = new AbortController()
    expect(await loadProbeHistory('task', 'node', '30d', controller.signal)).toEqual([month, week, current])
    expect(apiRequest).toHaveBeenCalledTimes(3)
    expect(vi.mocked(apiRequest).mock.calls.every(([, , options]) => options?.signal === controller.signal)).toBe(true)
  })

  it('rejects the complete range if one tier fails rather than returning misleading partial history', async () => {
    const failure = new APIError(503, 'telemetry_unavailable', 'History unavailable')
    vi.mocked(apiRequest).mockImplementation(async (path) => {
      if (new URLSearchParams(path.split('?')[1]).get('resolution') === '10m') throw failure
      return [point('2026-10-14T07:59:00Z')]
    })
    await expect(loadProbeHistory('task', 'node', '7d', new AbortController().signal)).rejects.toBe(failure)
    expect(apiRequest).toHaveBeenCalledTimes(2)
  })

  it('propagates unauthorized and cancellation failures without a successful partial result', async () => {
    const unauthorized = new APIError(401, 'unauthorized', 'Sign in')
    vi.mocked(apiRequest).mockRejectedValue(unauthorized)
    await expect(loadProbeHistory('task', 'node', '30d', new AbortController().signal)).rejects.toBe(unauthorized)
    const controller = new AbortController(); controller.abort()
    const cancelled = new DOMException('Aborted', 'AbortError')
    vi.mocked(apiRequest).mockRejectedValue(cancelled)
    await expect(loadProbeHistory('task', 'node', '30d', controller.signal)).rejects.toBe(cancelled)
  })
})
