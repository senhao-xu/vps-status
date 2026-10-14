import { type FormEvent, useEffect, useId, useRef, useState } from 'react'

import { APIError, apiMutation, errorMessage } from '../../api/client'
import { useI18n } from '../../i18n'

type PasswordError =
  | { kind: 'mismatch' }
  | { kind: 'current' }
  | { kind: 'changed' }
  | { kind: 'limited' }
  | { kind: 'request'; error: unknown }

export function ChangePasswordPanel({ onUnauthorized }: {
  onUnauthorized: (notice?: 'passwordChanged') => void
}) {
  const { t } = useI18n()
  const id = useId()
  const formRef = useRef<HTMLFormElement>(null)
  const request = useRef<AbortController | null>(null)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<PasswordError | null>(null)

  useEffect(() => {
    const form = formRef.current
    return () => {
      request.current?.abort()
      form?.reset()
    }
  }, [])

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (request.current) return
    const form = event.currentTarget
    const values = new FormData(form)
    const currentPassword = String(values.get('current_password') ?? '')
    const newPassword = String(values.get('new_password') ?? '')
    if (newPassword !== values.get('confirm_password')) {
      setError({ kind: 'mismatch' })
      return
    }
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(null)
    try {
      await apiMutation('/api/v1/admin/password', {
        signal: controller.signal,
        body: { current_password: currentPassword, new_password: newPassword },
      })
      if (controller.signal.aborted) return
      form.reset()
      onUnauthorized('passwordChanged')
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError) {
        if (caught.status === 401) { onUnauthorized(); return }
        if (caught.code === 'current_password_invalid') { setError({ kind: 'current' }); return }
        if (caught.code === 'auth_state_changed') { setError({ kind: 'changed' }); return }
        if (caught.status === 429) { setError({ kind: 'limited' }); return }
      }
      setError({ kind: 'request', error: caught })
    } finally {
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }

  const errorText = error === null ? ''
    : error.kind === 'mismatch' ? t('passwordsMismatch')
    : error.kind === 'current' ? t('currentPasswordInvalid')
    : error.kind === 'changed' ? t('passwordAuthStateChanged')
    : error.kind === 'limited' ? t('passwordRateLimited')
    : errorMessage(error.error, t)
  const description = `${id}-description${error ? ` ${id}-error` : ''}`

  return <section className="panel" aria-labelledby={`${id}-heading`}>
    <h2 id={`${id}-heading`}>{t('changePassword')}</h2>
    <p className="muted" id={`${id}-description`}>{t('changePasswordDescription')}</p>
    {error && <p className="notice error" id={`${id}-error`} role="alert">{errorText}</p>}
    <form ref={formRef} className="form-grid" onSubmit={(event) => void submit(event)} aria-busy={pending}>
      <label className="full">{t('currentPassword')}<input name="current_password" type="password" autoComplete="current-password" disabled={pending} aria-describedby={description} aria-invalid={error?.kind === 'current' || undefined} /></label>
      <label>{t('newPassword')}<input name="new_password" type="password" autoComplete="new-password" disabled={pending} aria-describedby={description} aria-invalid={error?.kind === 'mismatch' || undefined} /></label>
      <label>{t('confirmNewPassword')}<input name="confirm_password" type="password" autoComplete="new-password" disabled={pending} aria-describedby={description} aria-invalid={error?.kind === 'mismatch' || undefined} /></label>
      <div className="full settings-save-bar"><button className="primary" disabled={pending}>{pending ? t('changingPassword') : t('changePassword')}</button></div>
    </form>
  </section>
}
