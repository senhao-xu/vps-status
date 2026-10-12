import { describe, expect, it, vi } from 'vitest'

import { liveReconnectDelay, startNodeLiveStream } from './useNodeLiveStream'

class FakeSource {
  onopen: ((event: Event) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  readonly listeners = new Map<string, (event: Event) => void>()
  closed = false

  addEventListener(type: string, listener: (event: Event) => void) {
    this.listeners.set(type, listener)
  }

  close() {
    this.closed = true
  }

  emit(type: string, value: unknown) {
    this.listeners.get(type)?.(new MessageEvent(type, { data: JSON.stringify(value) }))
  }
}

async function flushPromises() {
  await Promise.resolve()
  await Promise.resolve()
}

describe('liveReconnectDelay', () => {
  it('uses bounded exponential-style retry delays', () => {
    expect([0, 1, 2, 3, 4, 5, 20].map(liveReconnectDelay)).toEqual([1000, 2000, 5000, 10000, 20000, 30000, 30000])
  })

  it('ignores callbacks from a replaced EventSource', async () => {
    const sources: FakeSource[] = []
    const timers: Array<() => void> = []
    const onEvent = vi.fn()
    const stop = startNodeLiveStream(onEvent, vi.fn(), {
      createSource: () => {
        const source = new FakeSource()
        sources.push(source)
        return source
      },
      checkSession: async () => 200,
      setTimer: (callback) => { timers.push(callback); return timers.length },
      clearTimer: vi.fn(),
    })
    sources[0]?.onerror?.(new Event('error'))
    await flushPromises()
    timers[0]?.()
    expect(sources).toHaveLength(2)

    const event = { kind: 'connected', node_id: 'node-1', connected: true, online: false, received_at: '2026-10-11T00:00:00Z' }
    sources[0]?.emit('connected', event)
    expect(onEvent).not.toHaveBeenCalled()
    sources[1]?.emit('connected', event)
    expect(onEvent).toHaveBeenCalledOnce()
    stop()
    expect(sources[1]?.closed).toBe(true)
  })

  it('closes the source, clears reconnect timers, and aborts a session recheck on cleanup', async () => {
    const source = new FakeSource()
    const clearTimer = vi.fn()
    let checkSignal: AbortSignal | undefined
    const stop = startNodeLiveStream(vi.fn(), vi.fn(), {
      createSource: () => source,
      checkSession: (signal) => {
        checkSignal = signal
        return new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError'))))
      },
      setTimer: () => 7,
      clearTimer,
    })
    source.onerror?.(new Event('error'))
    await flushPromises()
    stop()
    await flushPromises()
    expect(source.closed).toBe(true)
    expect(checkSignal?.aborted).toBe(true)
    expect(clearTimer).not.toHaveBeenCalled()

    const reconnectingSource = new FakeSource()
    const stopReconnect = startNodeLiveStream(vi.fn(), vi.fn(), {
      createSource: () => reconnectingSource,
      checkSession: async () => 200,
      setTimer: () => 9,
      clearTimer,
    })
    reconnectingSource.onerror?.(new Event('error'))
    await flushPromises()
    stopReconnect()
    expect(clearTimer).toHaveBeenCalledWith(9)
  })
})
