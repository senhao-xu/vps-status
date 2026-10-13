import { APIError, apiRequest } from '../../api/client'
import { decodeEnrollmentStatus, type EnrollmentStatus } from '../../api/contracts'

interface PollDependencies {
  read: (id: string, signal: AbortSignal) => Promise<EnrollmentStatus>
  setTimer: (callback: () => void, delay: number) => ReturnType<typeof setTimeout>
  clearTimer: (timer: ReturnType<typeof setTimeout>) => void
}

export function startEnrollmentPolling(id: string, onStatus: (status: EnrollmentStatus) => void, onError: (error: unknown, stopped: boolean) => void, dependencies?: PollDependencies): () => void {
  const deps: PollDependencies = dependencies ?? {
    read: (id, signal) => apiRequest(`/api/v1/admin/enrollments/${encodeURIComponent(id)}`, decodeEnrollmentStatus, { signal }),
    setTimer: (callback, delay) => setTimeout(callback, delay), clearTimer: (timer) => clearTimeout(timer),
  }
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopped = false
  let failures = 0
  let attempts = 0
  const stop = () => { stopped = true; controller.abort(); if (timer !== undefined) deps.clearTimer(timer) }
  const poll = async () => {
    if (stopped) return
    attempts++
    try {
      const status = await deps.read(id, controller.signal)
      if (stopped) return
      failures = 0
      onStatus(status)
      if (status.status === 'online' || status.status === 'expired') { stop(); return }
    } catch (error) {
      if (stopped) return
      failures++
      const terminal = error instanceof APIError && (error.status === 401 || error.status === 404) || failures >= 5
      onError(error, terminal)
      if (terminal) { stop(); return }
    }
    if (attempts >= 1800) { onError(new Error('enrollment_poll_limit'), true); stop(); return }
    if (!stopped) timer = deps.setTimer(() => void poll(), failures ? Math.min(30_000, 3000 * 2 ** failures) : 3000)
  }
  void poll()
  return stop
}
