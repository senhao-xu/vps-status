import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { APIError, apiRequest, errorMessage } from '../../api/client'
import { decodeEnrollment, decodeInstallationConfig, type Enrollment, type EnrollmentStatus, type InstallationConfig } from '../../api/contracts'
import { formatTime } from '../../format'
import { useI18n, type TranslationKey } from '../../i18n'
import { startEnrollmentPolling } from './enrollmentPolling'
import { revokeEnrollment } from './enrollmentActions'
import { resolveInstallationAddress } from './installationAddress'

const statusCopy: Record<EnrollmentStatus['status'], { title: TranslationKey; description: TranslationKey }> = {
  pending: { title: 'installationPending', description: 'pendingDescription' },
  registered: { title: 'installationRegistered', description: 'registeredDescription' },
  online: { title: 'installationOnline', description: 'onlineDescription' },
  expired: { title: 'installationExpired', description: 'expiredDescription' },
}

export function EnrollmentDialog({ onClose, onOnline, onViewNode, onUnauthorized, onOpenSettings }: {
  onClose: () => void; onOnline: (nodeId: string) => void; onViewNode: (nodeId: string) => void; onUnauthorized: () => void; onOpenSettings: () => void
}) {
  const { language, t } = useI18n()
  const dialog = useRef<HTMLDialogElement>(null)
  const nameInput = useRef<HTMLInputElement>(null)
  const controller = useRef<AbortController | null>(null)
  const stopPolling = useRef<(() => void) | null>(null)
  const notifiedNode = useRef('')
  const [name, setName] = useState('')
  const [config, setConfig] = useState<InstallationConfig | null>(null)
  const [configState, setConfigState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [configRevision, setConfigRevision] = useState(0)
  const translation = useRef(t)
  const url = config ? resolveInstallationAddress(config, typeof window === 'undefined' ? '' : window.location.origin) : null
  const [command, setCommand] = useState<Enrollment | null>(null)
  const [status, setStatus] = useState<EnrollmentStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [copy, setCopy] = useState('')
  const [pollRevision, setPollRevision] = useState(0)
  const terminal = status?.status === 'online' || status?.status === 'expired'

  useEffect(() => { translation.current = t }, [t])
  useEffect(() => {
    const request = new AbortController()
    setConfigState('loading'); setError('')
    void apiRequest('/api/v1/admin/installation', decodeInstallationConfig, { signal: request.signal }).then((value) => {
      if (request.signal.aborted) return
      setConfig(value); setConfigState('ready')
    }).catch((caught: unknown) => {
      if (request.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setConfigState('error'); setError(errorMessage(caught, translation.current))
    })
    return () => request.abort()
  }, [onUnauthorized, configRevision])

  useEffect(() => {
    const element = dialog.current
    const previous = document.activeElement
    element?.showModal()
    nameInput.current?.focus()
    return () => {
      controller.current?.abort(); stopPolling.current?.(); element?.close()
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [])

  const receiveStatus = useCallback((next: EnrollmentStatus) => {
    setStatus(next); setError('')
    if (next.status === 'online' && notifiedNode.current !== next.nodeId) {
      notifiedNode.current = next.nodeId; onOnline(next.nodeId)
    }
  }, [onOnline])

  useEffect(() => {
    if (!command || busy || terminal) return
    const stop = startEnrollmentPolling(command.id, receiveStatus, (caught, stopped) => {
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      if (caught instanceof APIError && caught.status === 404) { setError(t('installationStatusMissing')); return }
      setError(`${errorMessage(caught, t)} ${t(stopped ? 'pollStopped' : 'pollRetrying')}`)
    })
    stopPolling.current = stop
    return stop
  }, [command, busy, terminal, pollRevision, receiveStatus, onUnauthorized, t])

  const handleError = (caught: unknown) => {
    if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
    if (caught instanceof APIError && caught.status === 404) { setError(t('installationStatusMissing')); return }
    if (caught instanceof APIError && caught.code === 'agent_release_required') { setError(t('agentReleaseRequired')); return }
    setError(errorMessage(caught, t))
  }
  const create = async (regenerate = false) => {
    if (busy || controller.current || !config || configState !== 'ready' || !url || !config.releaseConfigured) return
    stopPolling.current?.()
    const request = new AbortController()
    controller.current = request
    setBusy(true); setError(''); setNotice(''); setCopy('')
    try {
      if (regenerate && command) {
        const result = await revokeEnrollment(command.id, request.signal)
        if (request.signal.aborted) return
        if (result.status === 'consumed') { receiveStatus(result.enrollment); setNotice(t('enrollmentConsumed')); return }
        setCommand(null); setStatus(null)
      }
      const next = await apiRequest('/api/v1/admin/enrollments', decodeEnrollment, {
        method: 'POST', signal: request.signal, body: { node_name: name, control_url: url, expires_in_seconds: 3600 },
      })
      if (request.signal.aborted) return
      setCommand(next); setStatus({ id: next.id, nodeName: name, expiresAt: next.expiresAt, status: 'pending' })
    } catch (caught) { if (!request.signal.aborted) handleError(caught) }
    finally { if (controller.current === request) controller.current = null; if (!request.signal.aborted) setBusy(false) }
  }
  const revoke = async () => {
    if (!command || busy || controller.current) return
    stopPolling.current?.()
    const request = new AbortController(); controller.current = request
    setBusy(true); setError(''); setNotice('')
    try {
      const result = await revokeEnrollment(command.id, request.signal)
      if (request.signal.aborted) return
      if (result.status === 'consumed') { receiveStatus(result.enrollment); setNotice(t('enrollmentConsumed')) }
      else { setCommand(null); setStatus(null); setCopy(''); setNotice(t('enrollmentRevoked')) }
    } catch (caught) {
      if (request.signal.aborted) return
      handleError(caught)
    } finally { if (controller.current === request) controller.current = null; if (!request.signal.aborted) setBusy(false) }
  }
  const copyCommand = async () => {
    if (!command) return
    try { await navigator.clipboard.writeText(command.installCommand); setCopy(t('installCommandCopied')) }
    catch { setCopy(t('copyCommandFailed')) }
  }
  const submit = (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); void create() }
  const phase = status?.status ?? 'pending'
  return <dialog className="enrollment-dialog" ref={dialog} aria-labelledby="enrollment-title" aria-describedby="enrollment-description" onCancel={(event) => { event.preventDefault(); onClose() }}>
    <header className="dialog-heading"><div><p className="eyebrow">{t('installation')}</p><h2 id="enrollment-title">{t('addNode')}</h2></div><button type="button" aria-label={t('close')} onClick={onClose}>×</button></header>
    <p id="enrollment-description" className="muted">{t('installationDescription')}</p>
    <ol className="install-steps" aria-label={t('installation')}><li className={!command ? 'current' : 'complete'}>{t('name')}</li><li className={command && phase !== 'online' ? 'current' : phase === 'online' ? 'complete' : ''}>{t('installStep')}</li><li className={phase === 'online' ? 'complete' : ''}>{t('readyStep')}</li></ol>
    {error && <p className="notice error" role="alert">{error}{command && <button type="button" disabled={busy} onClick={() => setPollRevision((value) => value + 1)}>{t('refreshStatus')}</button>}</p>}
    {notice && <p className="notice success" role="status">{notice}</p>}
    {!command ? <form className="form-grid" onSubmit={submit}>
      <label className="full">{t('displayName')}<input ref={nameInput} name="node_name" value={name} maxLength={128} required disabled={busy} onChange={(event) => setName(event.target.value)} /></label>
      {configState === 'loading' && <p className="full muted" role="status">{t('loadingInstallation')}</p>}
      {configState === 'error' && <button type="button" className="full" onClick={() => setConfigRevision((value) => value + 1)}>{t('retryInstallation')}</button>}
      {configState === 'ready' && config && <div className="full installation-context">
        {url ? <><p className="muted">{t('installConnectionSummary')}</p><dl className="installation-config-summary"><div><dt>{t('nodeConnectionUrl')}</dt><dd>{url}</dd></div><div><dt>{t('effectiveAgentVersion')}</dt><dd>{config.effectiveAgentReleaseVersion}</dd></div></dl></> : <p className="notice warning">{t('connectionAddressRequired')}</p>}
        {!config.releaseConfigured && <p className="notice warning">{t('agentReleaseRequired')}</p>}
        <button type="button" className="settings-link" onClick={onOpenSettings}>{t('openInstallationSettings')}</button>
      </div>}
      <div className="full dialog-actions"><button type="button" onClick={onClose}>{t('close')}</button><button className="primary" disabled={busy || configState !== 'ready' || !url || !config?.releaseConfigured}>{busy ? t('installInProgress') : t('createInstallCommand')}</button></div>
    </form> : <div className="installation-result">
      <div className={`installation-state ${phase}`} role="status"><span className="installation-dot" aria-hidden="true" /><div><strong>{t(statusCopy[phase].title)}</strong><p>{t(statusCopy[phase].description)}</p></div></div>
      <h3>{t('runInstallCommand')}</h3><p className="muted">{t('oneCommandInstall')}</p>
      <pre className="install-command" tabIndex={0}><code>{command.installCommand}</code></pre>
      <div className="command-meta"><button type="button" disabled={busy || phase === 'expired'} onClick={() => void copyCommand()}>{t('copyInstallCommand')}</button><span>{t('expires', { time: formatTime(command.expiresAt, language) })}</span></div>
      {copy && <p role="status">{copy}</p>}<p className="muted">{t('commandOnce')}</p>
      <details><summary>{t('manualEnrollCommand')}</summary><pre className="install-command"><code>{command.enrollCommand}</code></pre></details>
      <div className="button-row">
        {(phase === 'pending' || phase === 'expired') && <><button type="button" disabled={busy} onClick={() => void create(true)}>{t('regenerateCommand')}</button><button type="button" className="danger" disabled={busy} onClick={() => void revoke()}>{t('revokeInstallCommand')}</button></>}
        {status?.status === 'online' && <button className="primary" type="button" onClick={() => { onViewNode(status.nodeId); onClose() }}>{t('viewNode')}</button>}
      </div>
    </div>}
  </dialog>
}
