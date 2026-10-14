import { type FormEvent, useEffect, useRef, useState } from 'react'

import { APIError, apiRequest, errorMessage } from '../../api/client'
import { decodeInstallationConfig, type InstallationConfig } from '../../api/contracts'
import { useI18n } from '../../i18n'
import { isAgentReleaseVersion, isRemoteControlUrl } from './installationAddress'

type LoadState = { status: 'loading' } | { status: 'error' } | { status: 'ready'; config: InstallationConfig }

export function InstallationPanel({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { t } = useI18n()
  const heading = useRef<HTMLHeadingElement>(null)
  const translation = useRef(t)
  const readRequest = useRef<AbortController | null>(null)
  const saveRequest = useRef<AbortController | null>(null)
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [revision, setRevision] = useState(0)
  const [url, setUrl] = useState('')
  const [version, setVersion] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  useEffect(() => { heading.current?.focus() }, [])
  useEffect(() => { translation.current = t }, [t])
  useEffect(() => {
    const controller = new AbortController()
    readRequest.current = controller
    setLoad({ status: 'loading' }); setError('')
    void apiRequest('/api/v1/admin/installation', decodeInstallationConfig, { signal: controller.signal }).then((config) => {
      if (controller.signal.aborted) return
      setLoad({ status: 'ready', config }); setUrl(config.controlUrl); setVersion(config.agentReleaseVersion)
    }).catch((caught: unknown) => {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setLoad({ status: 'error' }); setError(errorMessage(caught, translation.current))
    })
    return () => { controller.abort(); saveRequest.current?.abort() }
  }, [onUnauthorized, revision])
  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (saveRequest.current || load.status !== 'ready') return
    const controlUrl = url.trim(), releaseVersion = version.trim()
    setError(''); setNotice('')
    if (controlUrl && !isRemoteControlUrl(controlUrl)) { setError(t('invalidConnectionUrl')); return }
    if (releaseVersion && !isAgentReleaseVersion(releaseVersion)) { setError(t('invalidAgentVersion')); return }
    readRequest.current?.abort()
    const controller = new AbortController()
    saveRequest.current = controller; setSaving(true)
    try {
      const config = await apiRequest('/api/v1/admin/installation', decodeInstallationConfig, {
        method: 'PUT', signal: controller.signal, body: { control_url: controlUrl, agent_release_version: releaseVersion },
      })
      if (controller.signal.aborted) return
      setLoad({ status: 'ready', config }); setUrl(config.controlUrl); setVersion(config.agentReleaseVersion)
      setNotice(translation.current('installationSaved'))
    } catch (caught) {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setError(errorMessage(caught, translation.current))
    } finally {
      if (saveRequest.current === controller) saveRequest.current = null
      if (!controller.signal.aborted) setSaving(false)
    }
  }
  const cancel = () => {
    if (load.status !== 'ready' || saving) return
    setUrl(load.config.controlUrl); setVersion(load.config.agentReleaseVersion); setError(''); setNotice('')
  }
  const dirty = load.status === 'ready' && (url !== load.config.controlUrl || version !== load.config.agentReleaseVersion)
  return <div className="stack installation-settings">
    <div className="section-heading"><div><h2 ref={heading} tabIndex={-1}>{t('installationSettings')}</h2><p className="muted">{t('installationSettingsDescription')}</p></div></div>
    {error && <p className="notice error" role="alert">{error}</p>}
    {notice && <p className="notice success" role="status">{notice}</p>}
    {load.status === 'loading' && <p role="status">{t('loadingInstallation')}</p>}
    {load.status === 'error' && <button type="button" onClick={() => setRevision((value) => value + 1)}>{t('retryInstallation')}</button>}
    {load.status === 'ready' && <form className="stack" onSubmit={(event) => void save(event)} aria-busy={saving}>
      <section className="panel settings-section" aria-labelledby="connection-heading">
        <div className="settings-section-heading"><h3 id="connection-heading">{t('nodeConnection')}</h3><p className="muted" id="connection-help">{t('connectionUrlHelp')}</p></div>
        <label>{t('nodeConnectionUrl')}<input name="control_url" type="url" placeholder="https://status.example.com" value={url} disabled={saving} aria-describedby="connection-help" onChange={(event) => { setUrl(event.target.value); setNotice('') }} /></label>
        {!load.config.controlUrl && <p className="muted">{t('automaticAddress')}</p>}
      </section>
      <section className="panel settings-section" aria-labelledby="download-heading">
        <div className="settings-section-heading"><h3 id="download-heading">{t('agentDownload')}</h3><p className="muted" id="release-help">{t('releaseVersionHelp')}</p></div>
        <label>{t('agentReleaseVersion')}<input name="agent_release_version" placeholder={load.config.effectiveAgentReleaseVersion} maxLength={64} value={version} disabled={saving} aria-describedby="release-help" onChange={(event) => { setVersion(event.target.value); setNotice('') }} /></label>
        <dl className="installation-config-summary"><div><dt>{t('effectiveAgentVersion')}</dt><dd><code>{load.config.effectiveAgentReleaseVersion}</code></dd></div><div><dt>{t('agentDownloadSource')}</dt><dd className="word-break">{load.config.agentReleaseBaseUrl}</dd></div></dl>
        {!load.config.releaseConfigured && <p className="notice warning">{t('agentReleaseRequired')}</p>}
      </section>
      <div className="settings-save-bar"><button className="primary" disabled={saving || !dirty}>{saving ? t('savingInstallation') : t('saveInstallation')}</button><button type="button" disabled={saving || !dirty} onClick={cancel}>{t('cancelChanges')}</button></div>
    </form>}
  </div>
}
