import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { APIError, apiMutation, apiRequest, errorMessage } from '../../api/client'
import { decodeAuthResponse, decodeSessionResponse, decodeSetupStatus, type SessionResponse } from '../../api/contracts'

interface AuthGateProps {
  children: (
    session: SessionResponse,
    refreshSession: () => Promise<void>,
    logout: () => Promise<void>,
    onUnauthorized: () => void,
  ) => React.ReactNode
}

type AuthState =
  | { status: 'checking' }
  | { status: 'setup' }
  | { status: 'login'; mfaRequired: boolean; error: string }
  | { status: 'authenticated'; session: SessionResponse }

export function AuthGate({ children }: AuthGateProps) {
  const [state, setState] = useState<AuthState>({ status: 'checking' })

  const refreshSession = useCallback(async () => {
    const session = await apiRequest('/api/v1/admin/session', decodeSessionResponse)
    setState({ status: 'authenticated', session })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    apiRequest('/api/v1/admin/setup', decodeSetupStatus, { signal: controller.signal })
      .then(async ({ configured }) => {
        if (!configured) {
          setState({ status: 'setup' })
          return
        }
        try {
          const session = await apiRequest('/api/v1/admin/session', decodeSessionResponse, { signal: controller.signal })
          setState({ status: 'authenticated', session })
        } catch (error) {
          if (!controller.signal.aborted) setState({ status: 'login', mfaRequired: false, error: error instanceof APIError && error.status !== 401 ? error.message : '' })
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setState({ status: 'login', mfaRequired: false, error: errorMessage(error) })
      })
    return () => controller.abort()
  }, [])

  const logout = useCallback(async () => {
    await apiMutation('/api/v1/admin/logout')
    setState({ status: 'login', mfaRequired: false, error: '' })
  }, [])

  const onUnauthorized = useCallback(() => {
    setState({ status: 'login', mfaRequired: false, error: '' })
  }, [])

  if (state.status === 'checking') return <main className="auth-shell"><p role="status">Checking Control session…</p></main>
  if (state.status === 'authenticated') return <>{children(state.session, refreshSession, logout, onUnauthorized)}</>
  if (state.status === 'setup') {
    return <SetupForm onAuthenticated={(session) => setState({ status: 'authenticated', session })} />
  }
  return <LoginForm state={state} onState={setState} />
}

function SetupForm({ onAuthenticated }: { onAuthenticated: (session: SessionResponse) => void }) {
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setPending(true)
    setError('')
    try {
      await apiRequest('/api/v1/admin/setup', decodeAuthResponse, {
        method: 'POST', body: { username: form.get('username'), password: form.get('password') },
      })
      const session = await apiRequest('/api/v1/admin/session', decodeSessionResponse)
      onAuthenticated(session)
    } catch (caught) {
      setError(errorMessage(caught))
    } finally {
      setPending(false)
    }
  }
  return (
    <main className="auth-shell">
      <form className="auth-card" onSubmit={submit}>
        <p className="eyebrow">First run</p><h1>Create the administrator</h1>
        <p className="muted">This Control has exactly one administrator account.</p>
        {error && <p className="notice error" role="alert">{error}</p>}
        <label>Username<input name="username" minLength={3} maxLength={64} autoComplete="username" required /></label>
        <label>Password<input name="password" type="password" minLength={12} maxLength={1024} autoComplete="new-password" required /></label>
        <button className="primary" disabled={pending}>{pending ? 'Creating…' : 'Create administrator'}</button>
      </form>
    </main>
  )
}

function LoginForm({ state, onState }: {
  state: Extract<AuthState, { status: 'login' }>
  onState: (state: AuthState) => void
}) {
  const [pending, setPending] = useState(false)
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    setPending(true)
    try {
      await apiRequest('/api/v1/admin/login', decodeAuthResponse, {
        method: 'POST', body: { username: form.get('username'), password: form.get('password'), mfa_code: form.get('mfa_code') ?? '' },
      })
      const session = await apiRequest('/api/v1/admin/session', decodeSessionResponse)
      onState({ status: 'authenticated', session })
    } catch (caught) {
      if (caught instanceof APIError && caught.code === 'mfa_required') {
        onState({ status: 'login', mfaRequired: true, error: 'Enter a TOTP or unused recovery code.' })
      } else {
        onState({ status: 'login', mfaRequired: state.mfaRequired, error: errorMessage(caught) })
      }
    } finally {
      setPending(false)
    }
  }
  return (
    <main className="auth-shell">
      <form className="auth-card" onSubmit={submit}>
        <p className="eyebrow">Control</p><h1>Administrator login</h1>
        {state.error && <p className="notice error" role="alert">{state.error}</p>}
        <label>Username<input name="username" autoComplete="username" required /></label>
        <label>Password<input name="password" type="password" autoComplete="current-password" required /></label>
        {state.mfaRequired && <label>MFA or recovery code<input name="mfa_code" inputMode="numeric" autoComplete="one-time-code" required autoFocus /></label>}
        <button className="primary" disabled={pending}>{pending ? 'Signing in…' : 'Sign in'}</button>
        <a href="/public">Open public status</a>
      </form>
    </main>
  )
}
