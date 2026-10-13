import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { APIError, apiMutation, apiRequest, errorMessage } from '../../api/client'
import { decodeAuthResponse, decodeSessionResponse, decodeSetupStatus, type SessionResponse } from '../../api/contracts'
import { PreferenceControls } from '../../components/PreferenceControls'
import { useI18n } from '../../i18n'

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
  const { t } = useI18n()
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
          if (!controller.signal.aborted) setState({
            status: 'login',
            mfaRequired: false,
            error: error instanceof APIError && error.status === 401 ? '' : errorMessage(error, t),
          })
        }
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setState({ status: 'login', mfaRequired: false, error: errorMessage(error, t) })
      })
    return () => controller.abort()
  }, [t])

  const logout = useCallback(async () => {
    await apiMutation('/api/v1/admin/logout')
    setState({ status: 'login', mfaRequired: false, error: '' })
  }, [])

  const onUnauthorized = useCallback(() => {
    setState({ status: 'login', mfaRequired: false, error: '' })
  }, [])

  if (state.status === 'checking') return <main className="auth-shell"><PreferenceControls /><p role="status">{t('checkingSession')}</p></main>
  if (state.status === 'authenticated') return <>{children(state.session, refreshSession, logout, onUnauthorized)}</>
  if (state.status === 'setup') {
    return <SetupForm onAuthenticated={(session) => setState({ status: 'authenticated', session })} />
  }
  return <LoginForm state={state} onState={setState} />
}

function SetupForm({ onAuthenticated }: { onAuthenticated: (session: SessionResponse) => void }) {
  const { t } = useI18n()
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
      setError(errorMessage(caught, t))
    } finally {
      setPending(false)
    }
  }
  return (
    <main className="auth-shell">
      <PreferenceControls />
      <form className="auth-card" onSubmit={submit}>
        <p className="eyebrow">{t('firstRun')}</p><h1>{t('createAdministrator')}</h1>
        <p className="muted">{t('singleAdministrator')}</p>
        {error && <p className="notice error" role="alert">{error}</p>}
        <label>{t('username')}<input name="username" minLength={3} maxLength={64} autoComplete="username" required /></label>
        <label>{t('password')}<input name="password" type="password" autoComplete="new-password" /></label>
        <button className="primary" disabled={pending}>{pending ? t('creating') : t('createAdministrator')}</button>
      </form>
    </main>
  )
}

function LoginForm({ state, onState }: {
  state: Extract<AuthState, { status: 'login' }>
  onState: (state: AuthState) => void
}) {
  const { t } = useI18n()
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
        onState({ status: 'login', mfaRequired: true, error: t('enterMfa') })
      } else {
        onState({ status: 'login', mfaRequired: state.mfaRequired, error: errorMessage(caught, t) })
      }
    } finally {
      setPending(false)
    }
  }
  return (
    <main className="auth-shell">
      <PreferenceControls />
      <form className="auth-card" onSubmit={submit}>
        <p className="eyebrow">Control</p><h1>{t('administratorLogin')}</h1>
        {state.error && <p className="notice error" role="alert">{state.error}</p>}
        <label>{t('username')}<input name="username" autoComplete="username" required /></label>
        <label>{t('password')}<input name="password" type="password" autoComplete="current-password" /></label>
        {state.mfaRequired && <label>{t('mfaOrRecoveryCode')}<input name="mfa_code" inputMode="numeric" autoComplete="one-time-code" required autoFocus /></label>}
        <button className="primary" disabled={pending}>{pending ? t('signingIn') : t('signIn')}</button>
        <a href="/public">{t('openPublicStatus')}</a>
      </form>
    </main>
  )
}
