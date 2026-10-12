import { useEffect } from 'react'

import { decodeLiveEvent, type LiveEvent } from '../../api/contracts'

const reconnectDelays = [1_000, 2_000, 5_000, 10_000, 20_000, 30_000] as const

export function liveReconnectDelay(attempt: number): number {
  return reconnectDelays[Math.min(Math.max(0, attempt), reconnectDelays.length - 1)]
}

interface LiveEventSource {
  onopen: ((event: Event) => void) | null
  onerror: ((event: Event) => void) | null
  addEventListener(type: string, listener: (event: Event) => void): void
  close(): void
}

interface LiveStreamRuntime {
  createSource(): LiveEventSource
  checkSession(signal: AbortSignal): Promise<number>
  setTimer(callback: () => void, delay: number): number
  clearTimer(timer: number): void
}

function browserRuntime(): LiveStreamRuntime {
  return {
    createSource: () => new EventSource('/api/v1/admin/live', { withCredentials: true }),
    checkSession: async (signal) => {
      const response = await fetch('/api/v1/admin/session', { credentials: 'same-origin', signal })
      return response.status
    },
    setTimer: (callback, delay) => window.setTimeout(callback, delay),
    clearTimer: (timer) => window.clearTimeout(timer),
  }
}

export function startNodeLiveStream(
  onEvent: (event: LiveEvent) => void,
  onUnauthorized: () => void,
  runtime: LiveStreamRuntime = browserRuntime(),
): () => void {
  let disposed = false
  let source: LiveEventSource | null = null
  let reconnectTimer: number | null = null
  let attempts = 0
  let checkingSession = false
  let sessionCheckController: AbortController | null = null

  const scheduleReconnect = () => {
    if (disposed || reconnectTimer !== null) return
    source?.close()
    source = null
    const delay = liveReconnectDelay(attempts)
    attempts += 1
    reconnectTimer = runtime.setTimer(() => {
      reconnectTimer = null
      connect()
    }, delay)
  }

  const connect = () => {
    if (disposed) return
    const currentSource = runtime.createSource()
    source = currentSource
    currentSource.onopen = () => {
      if (source !== currentSource) return
      attempts = 0
    }
    for (const kind of ['connected', 'report', 'stale', 'disconnected', 'deleted']) {
      currentSource.addEventListener(kind, (raw) => {
        if (disposed || source !== currentSource || !(raw instanceof MessageEvent)) return
        try {
          const parsed: unknown = JSON.parse(String(raw.data))
          onEvent(decodeLiveEvent(parsed))
        } catch {
          currentSource.close()
          if (source === currentSource) source = null
          scheduleReconnect()
        }
      })
    }
    currentSource.onerror = async () => {
      if (disposed || source !== currentSource) return
      currentSource.close()
      source = null
      if (checkingSession || reconnectTimer !== null) return
      checkingSession = true
      const controller = new AbortController()
      sessionCheckController = controller
      try {
        const status = await runtime.checkSession(controller.signal)
        if (!disposed && status === 401) {
          onUnauthorized()
          return
        }
      } catch {
        // A transient network failure follows the same bounded reconnect path.
      } finally {
        if (sessionCheckController === controller) sessionCheckController = null
        checkingSession = false
      }
      scheduleReconnect()
    }
  }

  connect()
  return () => {
    disposed = true
    source?.close()
    sessionCheckController?.abort()
    if (reconnectTimer !== null) runtime.clearTimer(reconnectTimer)
  }
}

export function useNodeLiveStream(active: boolean, onEvent: (event: LiveEvent) => void, onUnauthorized: () => void) {
  useEffect(() => {
    if (!active) return
    return startNodeLiveStream(onEvent, onUnauthorized)
  }, [active, onEvent, onUnauthorized])
}
