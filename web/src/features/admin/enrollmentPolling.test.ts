import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIError } from '../../api/client'
import { type EnrollmentStatus } from '../../api/contracts'
import { startEnrollmentPolling } from './enrollmentPolling'

const base = { id: 'enrollment-1', nodeName: 'Edge', expiresAt: '2026-10-13T12:00:00Z' }
const pending: EnrollmentStatus = { ...base, status: 'pending' }
const registered: EnrollmentStatus = { ...base, status: 'registered', nodeId: 'node-1' }
const online: EnrollmentStatus = { ...base, status: 'online', nodeId: 'node-1' }

afterEach(() => vi.useRealTimers())

function dependencies(read: (id: string, signal: AbortSignal) => Promise<EnrollmentStatus>) {
  return { read, setTimer: (callback: () => void, delay: number) => setTimeout(callback, delay), clearTimer: (timer: ReturnType<typeof setTimeout>) => clearTimeout(timer) }
}

describe('enrollment polling lifecycle', () => {
  it('advances by enrollment ID and stops when the new node becomes online', async () => {
    vi.useFakeTimers()
    const read = vi.fn().mockResolvedValueOnce(pending).mockResolvedValueOnce(registered).mockResolvedValueOnce(online)
    const receive = vi.fn(), error = vi.fn()
    const stop = startEnrollmentPolling(base.id, receive, error, dependencies(read))
    await vi.advanceTimersByTimeAsync(0)
    expect(receive).toHaveBeenLastCalledWith(pending)
    await vi.advanceTimersByTimeAsync(3000)
    expect(receive).toHaveBeenLastCalledWith(registered)
    await vi.advanceTimersByTimeAsync(3000)
    expect(receive).toHaveBeenLastCalledWith(online)
    await vi.advanceTimersByTimeAsync(90_000)
    expect(read).toHaveBeenCalledTimes(3)
    expect(read.mock.calls.every((call) => call[0] === base.id)).toBe(true)
    expect(error).not.toHaveBeenCalled()
    stop()
  })

  it('stops immediately on server expiry even if the browser clock would allow installation', async () => {
    vi.useFakeTimers()
    const receive = vi.fn(), read = vi.fn().mockResolvedValue({ ...base, status: 'expired' })
    startEnrollmentPolling(base.id, receive, vi.fn(), dependencies(read))
    await vi.advanceTimersByTimeAsync(90_000)
    expect(read).toHaveBeenCalledOnce()
    expect(receive).toHaveBeenLastCalledWith({ ...base, status: 'expired' })
  })

  it('never overlaps slow requests and ignores late responses after dialog cleanup', async () => {
    vi.useFakeTimers()
    let complete: ((value: EnrollmentStatus) => void) | undefined
    let requestSignal: AbortSignal | undefined
    const read = vi.fn((_id: string, signal: AbortSignal) => {
      requestSignal = signal
      return new Promise<EnrollmentStatus>((resolve) => { complete = resolve })
    })
    const receive = vi.fn(), error = vi.fn()
    const stop = startEnrollmentPolling(base.id, receive, error, dependencies(read))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(read).toHaveBeenCalledOnce()
    stop()
    expect(requestSignal?.aborted).toBe(true)
    complete?.(online)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(receive).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cancels the scheduled next check when the dialog closes or a command is replaced', async () => {
    vi.useFakeTimers()
    const read = vi.fn().mockResolvedValue(pending)
    const stop = startEnrollmentPolling(base.id, vi.fn(), vi.fn(), dependencies(read))
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(1)
    stop()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(read).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('backs off transient errors, reports retries truthfully, and stops after five failures', async () => {
    vi.useFakeTimers()
    const unavailable = new APIError(503, 'unavailable', 'Unavailable')
    const read = vi.fn().mockRejectedValue(unavailable), error = vi.fn()
    startEnrollmentPolling(base.id, vi.fn(), error, dependencies(read))
    await vi.advanceTimersByTimeAsync(0)
    expect(error).toHaveBeenLastCalledWith(unavailable, false)
    expect(read).toHaveBeenCalledOnce()
    await vi.advanceTimersByTimeAsync(5999)
    expect(read).toHaveBeenCalledOnce()
    await vi.advanceTimersByTimeAsync(1)
    expect(read).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(120_000)
    expect(read).toHaveBeenCalledTimes(5)
    expect(error).toHaveBeenLastCalledWith(unavailable, true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([401, 404])('stops without retry on %s', async (code) => {
    vi.useFakeTimers()
    const failure = new APIError(code, 'not_found', 'Unavailable')
    const read = vi.fn().mockRejectedValue(failure), error = vi.fn()
    startEnrollmentPolling(base.id, vi.fn(), error, dependencies(read))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(read).toHaveBeenCalledOnce()
    expect(error).toHaveBeenCalledWith(failure, true)
    expect(vi.getTimerCount()).toBe(0)
  })
})
