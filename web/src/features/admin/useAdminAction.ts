import { useEffect, useRef, useState } from 'react'

import { APIError, errorMessage } from '../../api/client'
import { useI18n } from '../../i18n'

type ActionState =
  | { status: 'idle' | 'pending' }
  | { status: 'success'; message: string }
  | { status: 'error'; message: string }

// Each settings panel owns one cancellable write; failures keep its draft intact.
export function useAdminAction(onUnauthorized: () => void) {
  const { t } = useI18n()
  const request = useRef<AbortController | null>(null)
  const [state, setState] = useState<ActionState>({ status: 'idle' })
  useEffect(() => () => request.current?.abort(), [])
  const run = async <T,>(action: (signal: AbortSignal) => Promise<T>, success: (value: T) => void, message = '') => {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setState({ status: 'pending' })
    try {
      const result = await action(controller.signal)
      if (controller.signal.aborted) return
      success(result)
      setState({ status: 'success', message })
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setState({ status: 'error', message: errorMessage(caught, t) })
    } finally {
      if (request.current === controller) request.current = null
    }
  }
  return { pending: state.status === 'pending', error: state.status === 'error' ? state.message : '', notice: state.status === 'success' ? state.message : '', run }
}
