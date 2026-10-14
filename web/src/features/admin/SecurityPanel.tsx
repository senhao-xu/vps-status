import { type FormEvent, useState } from 'react'

import { apiMutation, apiRequest } from '../../api/client'
import { decodeRecoveryCodes, decodeTOTPSetup, type SessionResponse } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { ChangePasswordPanel } from './ChangePasswordPanel'
import { SettingsFeedback } from './SettingsFeedback'
import { useAdminAction } from './useAdminAction'

export function SecurityPanel({ session, refreshSession, onUnauthorized }: { session: SessionResponse; refreshSession: () => Promise<void>; onUnauthorized: (notice?: 'passwordChanged') => void }) {
  const { t } = useI18n()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [recovery, setRecovery] = useState<string[]>([])
  const [disabling, setDisabling] = useState(false)
  const [committedMfa, setCommittedMfa] = useState<boolean | null>(null)
  const mfaEnabled = committedMfa ?? session.admin.mfaEnabled
  const action = useAdminAction(onUnauthorized)
  const syncSession = async (signal: AbortSignal) => {
    await refreshSession()
    if (!signal.aborted) setCommittedMfa(null)
  }
  const retrySession = () => void action.run(syncSession, () => {}, t(committedMfa ? 'mfaEnabledNotice' : 'mfaDisabled'))
  const start = () => void action.run((signal) => apiRequest('/api/v1/admin/mfa/totp/setup', decodeTOTPSetup, { method: 'POST', signal }), (value) => { setRecovery([]); setSetup(value) })
  const confirm = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const code = new FormData(event.currentTarget).get('code')
    void action.run(async (signal) => {
      const codes = await apiRequest('/api/v1/admin/mfa/totp/confirm', decodeRecoveryCodes, { method: 'POST', signal, body: { code } })
      if (signal.aborted) return
      // Preserve the one-time response even if refreshing session status fails.
      setRecovery(codes); setSetup(null); setCommittedMfa(true)
      await syncSession(signal)
    }, () => {}, t('mfaEnabledNotice'))
  }
  const disable = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const element = event.currentTarget, form = new FormData(element)
    void action.run(async (signal) => {
      await apiMutation('/api/v1/admin/mfa/totp/disable', { signal, body: { password: form.get('password'), code: form.get('code') } })
      if (signal.aborted) return
      element.reset(); setRecovery([]); setDisabling(false); setCommittedMfa(false)
      await syncSession(signal)
    }, () => {}, t('mfaDisabled'))
  }
  return <div className="settings-stack">
    <section className="panel settings-intro"><div className="section-heading"><div><h2>{t('accountOverview')}</h2><p className="muted">{t('accountSecurityHint')}</p></div><span className={`status ${mfaEnabled ? 'online' : 'offline'}`}>{t(mfaEnabled ? 'mfaProtected' : 'mfaNotEnabled')}</span></div><dl className="settings-summary"><div><dt>{t('username')}</dt><dd>{session.admin.username}</dd></div><div><dt>{t('multiFactorAuthentication')}</dt><dd>{t(mfaEnabled ? 'enabled' : 'disabled')}</dd></div></dl></section>
    <ChangePasswordPanel onUnauthorized={onUnauthorized} />
    <section className="panel" aria-busy={action.pending}><div className="section-heading"><div><h2>{t('multiFactorAuthentication')}</h2><p className="muted">{t('totpHint')}</p></div><span className={`status ${mfaEnabled ? 'online' : 'offline'}`}>{t(mfaEnabled ? 'enabled' : 'disabled')}</span></div><SettingsFeedback {...action} />
      {committedMfa !== null && <div className="notice warning"><p>{t('mfaRefreshNeeded')}</p><button type="button" disabled={action.pending} onClick={retrySession}>{t('refreshStatus')}</button></div>}
      {!mfaEnabled && !setup && recovery.length === 0 && <button className="primary" disabled={action.pending || committedMfa !== null} onClick={start}>{t(action.pending ? 'savingSettings' : 'setupTotp')}</button>}
      {setup && <div className="secret-once"><p>{t('authenticatorSecret')}</p><code>{setup.secret}</code><p className="word-break">{setup.uri}</p><form className="form-grid" onSubmit={confirm}><label className="full">{t('sixDigitCode')}<input name="code" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" required disabled={action.pending} /></label><div className="full settings-save-bar"><button type="button" disabled={action.pending} onClick={() => setSetup(null)}>{t('cancelChanges')}</button><button className="primary" disabled={action.pending}>{t(action.pending ? 'savingSettings' : 'confirmTotp')}</button></div></form></div>}
      {recovery.length > 0 && <div className="secret-once"><h3>{t('recoveryCodes')}</h3><p className="muted">{t('recoveryCodesHint')}</p><ul className="recovery-code-list">{recovery.map((code) => <li key={code}><code>{code}</code></li>)}</ul><button type="button" disabled={action.pending} onClick={() => setRecovery([])}>{t('savedRecoveryCodes')}</button></div>}
      {mfaEnabled && !disabling && <button className="danger" disabled={action.pending || committedMfa !== null} onClick={() => setDisabling(true)}>{t('disableMfa')}</button>}
      {mfaEnabled && disabling && <form onSubmit={disable} className="form-grid"><p className="full muted">{t('disableMfaHint')}</p><label>{t('password')}<input name="password" type="password" autoComplete="current-password" disabled={action.pending} /></label><label>{t('mfaOrRecoveryCode')}<input name="code" autoComplete="one-time-code" required disabled={action.pending} /></label><div className="full settings-save-bar"><button type="button" disabled={action.pending} onClick={() => setDisabling(false)}>{t('cancelChanges')}</button><button className="danger" disabled={action.pending}>{t(action.pending ? 'savingSettings' : 'disableMfa')}</button></div></form>}
      <p className="muted settings-footnote">{t('mfaRecoveryNotice')}</p>
    </section>
  </div>
}
