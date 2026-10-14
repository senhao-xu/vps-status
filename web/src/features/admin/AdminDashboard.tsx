import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'

import { APIError, apiMutation, apiRequest, errorMessage } from '../../api/client'
import {
  decodeChannels, decodeNodes, decodeProbes,
  decodePublicPageConfig, decodeRecoveryCodes, decodeRules, decodeStorageStats, decodeTOTPSetup,
  reduceNodeEvent, type AlertRule, type Node, type NotificationChannel, type ProbeTask,
  type PublicPageConfig, type SessionResponse, type StorageStats, type TrafficTotal,
} from '../../api/contracts'
import { PreferenceControls } from '../../components/PreferenceControls'
import { formatBytes, formatTime } from '../../format'
import { useI18n } from '../../i18n'
import { useNodeLiveStream } from '../live/useNodeLiveStream'
import { AppearancePanel } from './AppearancePanel'
import { ChangePasswordPanel } from './ChangePasswordPanel'
import { InstallationPanel } from './InstallationPanel'
import { NodesPanel } from './NodesPanel'
import { ProbesPanel } from './ProbesPanel'
import { createNodeRefreshBuffer } from './nodeRefresh'

type Section = 'nodes' | 'probes' | 'alerts' | 'storage' | 'public' | 'security' | 'appearance' | 'installation'

interface DashboardData {
  nodes: Node[]
  probes: ProbeTask[]
  channels: NotificationChannel[]
  rules: AlertRule[]
  storage: StorageStats
  publicPage: PublicPageConfig
}

interface AdminDashboardProps {
  session: SessionResponse
  refreshSession: () => Promise<void>
  logout: () => Promise<void>
  onUnauthorized: (notice?: 'passwordChanged') => void
}

export function AdminDashboard({ session, refreshSession, logout, onUnauthorized }: AdminDashboardProps) {
  const { t } = useI18n()
  const [section, setSection] = useState<Section>('nodes')
  const [data, setData] = useState<DashboardData | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [revision, setRevision] = useState(0)
  const refreshNodes = useCallback(() => setRevision((value) => value + 1), [])
  const pendingNodeLoad = useRef<{ controller: AbortController; buffer: ReturnType<typeof createNodeRefreshBuffer> } | null>(null)

  const load = useCallback(async (controller: AbortController) => {
    const { signal } = controller
    const pending = { controller, buffer: createNodeRefreshBuffer() }
    pendingNodeLoad.current = pending
    try {
      const [nodes, probes, channels, rules, storage, publicPage] = await Promise.all([
        apiRequest('/api/v1/admin/nodes', decodeNodes, { signal }),
        apiRequest('/api/v1/admin/probes', decodeProbes, { signal }),
        apiRequest('/api/v1/admin/notification-channels', decodeChannels, { signal }),
        apiRequest('/api/v1/admin/alert-rules', decodeRules, { signal }),
        apiRequest('/api/v1/admin/telemetry/storage', decodeStorageStats, { signal }),
        apiRequest('/api/v1/admin/public-page', decodePublicPageConfig, { signal }),
      ])
      if (signal.aborted) return
      setData({ nodes: pending.buffer.replay(nodes), probes, channels, rules, storage, publicPage })
      setError('')
    } catch (caught) {
      if (signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) {
        onUnauthorized()
        return
      }
      setError(errorMessage(caught, t))
    } finally {
      if (pendingNodeLoad.current === pending) pendingNodeLoad.current = null
    }
  }, [onUnauthorized, t])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller)
    return () => controller.abort()
  }, [load, revision])

  const onLiveEvent = useCallback((event: Parameters<typeof reduceNodeEvent>[1]) => {
    const pending = pendingNodeLoad.current
    if (pending && !pending.controller.signal.aborted && !pending.buffer.record(event)) {
      // Restart the snapshot rather than discard live events if a request stalls.
      pending.controller.abort()
      pendingNodeLoad.current = null
      refreshNodes()
    }
    setData((current) => current ? { ...current, nodes: reduceNodeEvent(current.nodes, event) } : current)
  }, [refreshNodes])
  useNodeLiveStream(data !== null, onLiveEvent, onUnauthorized)

  const mutate = async (action: () => Promise<void>, success: string) => {
    setError('')
    setNotice('')
    try {
      await action()
      setNotice(success)
      setRevision((value) => value + 1)
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) {
        onUnauthorized()
        return
      }
      setError(errorMessage(caught, t))
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="/">VPS <span>Status</span></a>
        <nav aria-label={t('adminSections')}>
          {(['nodes', 'probes', 'alerts', 'storage', 'public', 'installation', 'appearance', 'security'] as const).map((item) => (
            <button key={item} className={section === item ? 'active' : ''} aria-current={section === item ? 'page' : undefined} onClick={() => setSection(item)}>
              {sectionLabel(item, t)}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer"><PreferenceControls /><span>{session.admin.username}</span><button onClick={() => { void logout().catch((caught: unknown) => setError(errorMessage(caught, t))) }}>{t('signOut')}</button></div>
      </aside>
      <main className="content" id="main-content">
        <header className="content-header">
          <div><p className="eyebrow">Control</p><h1>{sectionLabel(section, t)}</h1></div>
          <a href="/" target="_blank" rel="noreferrer">{t('publicPageNewWindow')}</a>
        </header>
        {error && <p className="notice error" role="alert">{error}</p>}
        {notice && <p className="notice success" role="status">{notice}</p>}
        {section === 'installation' ? <InstallationPanel onUnauthorized={onUnauthorized} /> : !data ? <p role="status">{t('loadingAdmin')}</p> : (
          <>
            {section === 'nodes' && <NodesPanel nodes={data.nodes} mutate={mutate} refreshNodes={refreshNodes} onUnauthorized={onUnauthorized} onOpenSettings={() => setSection('installation')} />}
            {section === 'probes' && <ProbesPanel probes={data.probes} nodes={data.nodes} refresh={refreshNodes} onUnauthorized={onUnauthorized} />}
            {section === 'alerts' && <AlertsPanel data={data} mutate={mutate} />}
            {section === 'storage' && <StoragePanel storage={data.storage} mutate={mutate} />}
            {section === 'public' && <PublicConfigPanel data={data} mutate={mutate} />}
            {section === 'security' && <><ChangePasswordPanel onUnauthorized={onUnauthorized} /><SecurityPanel session={session} refreshSession={refreshSession} mutate={mutate} /></>}
            {section === 'appearance' && <AppearancePanel onUnauthorized={onUnauthorized} />}
          </>
        )}
      </main>
    </div>
  )
}

type Translate = ReturnType<typeof useI18n>['t']

function sectionLabel(section: Section, t: Translate): string {
  switch (section) {
    case 'nodes': return t('nodes')
    case 'probes': return t('tcpProbes')
    case 'alerts': return t('alerts')
    case 'storage': return t('telemetryStorage')
    case 'public': return t('publicPage')
    case 'security': return t('security')
    case 'appearance': return t('themeManagement')
    case 'installation': return t('installationSettings')
  }
}

type Mutate = (action: () => Promise<void>, success: string) => Promise<void>


function AlertsPanel({ data, mutate }: { data: DashboardData; mutate: Mutate }) {
  const { t } = useI18n()
  const createChannel = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget); const kind = String(form.get('kind'))
    const body = kind === 'webhook'
      ? { name: form.get('name'), kind, enabled: true, url: form.get('url'), secret: form.get('secret') }
      : { name: form.get('name'), kind, enabled: true, bot_token: form.get('bot_token'), chat_id: form.get('chat_id') }
    return mutate(() => apiMutation('/api/v1/admin/notification-channels', { body }), t('channelCreated'))
  }
  const createRule = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget); const metric = String(form.get('metric'))
    return mutate(() => apiMutation('/api/v1/admin/alert-rules', { body: {
      name: form.get('name'), scope_kind: form.get('scope_kind'), scope_value: form.get('scope_value'), metric,
      threshold: metric === 'offline' ? 0 : Number(form.get('threshold')), duration_seconds: Number(form.get('duration')),
      recovery_hysteresis: metric === 'offline' ? 0 : Number(form.get('hysteresis')), enabled: true,
      reminder_interval_seconds: 0, reminder_max_count: 0, channel_ids: form.getAll('channel_ids').map(String),
    } }), t('ruleCreated'))
  }
  return (
    <div className="two-column">
      <section className="panel"><h2>{t('notificationChannels')}</h2><p className="muted">{t('secretsEncrypted')}</p>
        <form className="form-grid" onSubmit={(event) => void createChannel(event)}><label>{t('name')}<input name="name" required /></label><label>{t('kind')}<select name="kind" defaultValue="webhook"><option value="webhook">{t('webhook')}</option><option value="telegram">{t('telegram')}</option></select></label><label>{t('webhookUrl')}<input name="url" type="url" placeholder="https://…" /></label><label>{t('webhookSecret')}<input name="secret" type="password" /></label><label>{t('telegramToken')}<input name="bot_token" type="password" /></label><label>{t('telegramChatId')}<input name="chat_id" type="password" /></label><button>{t('createChannel')}</button></form>
        <div className="list">{data.channels.map((channel) => <article className="list-item" key={channel.id}><div className="item-main"><strong>{channel.name}</strong><span>{channel.kind === 'webhook' ? t('webhook') : t('telegram')} · {channel.configured ? t('configured') : t('incomplete')} · {channel.enabled ? t('enabled') : t('disabled')}</span></div><div className="button-row"><button onClick={() => void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}`, { method: 'PATCH', body: { enabled: !channel.enabled } }), t(channel.enabled ? 'channelDisabled' : 'channelEnabled'))}>{channel.enabled ? t('disable') : t('enable')}</button><button disabled={!channel.enabled} onClick={() => void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}/test`), t('testQueued'))}>{t('test')}</button><button className="danger" onClick={() => { if (window.confirm(t('confirmDeleteChannel', { name: channel.name }))) void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}`, { method: 'DELETE' }), t('channelDeleted')) }}>{t('delete')}</button></div></article>)}</div>
      </section>
      <section className="panel"><h2>{t('alertRules')}</h2><form className="form-grid" onSubmit={(event) => void createRule(event)}><label>{t('name')}<input name="name" required /></label><label>{t('scope')}<select name="scope_kind"><option value="node">{t('node')}</option><option value="group">{t('group')}</option></select></label><label>{t('nodeIdOrGroup')}<input name="scope_value" list="alert-scopes" required /><datalist id="alert-scopes">{data.nodes.flatMap((node) => [<option key={node.id} value={node.id}>{node.name}</option>, <option key={`${node.id}-group`} value={node.group}>{node.group}</option>])}</datalist></label><label>{t('metric')}<select name="metric"><option value="offline">{t('offline')}</option><option value="cpu">CPU %</option><option value="memory">{t('memoryPercent')}</option><option value="disk">{t('diskPercent')}</option><option value="traffic_rate">{t('trafficRate')}</option></select></label><label>{t('threshold')}<input name="threshold" type="number" min={0} defaultValue={90} /></label><label>{t('durationSeconds')}<input name="duration" type="number" min={0} max={86400} defaultValue={60} /></label><label>{t('recoveryHysteresis')}<input name="hysteresis" type="number" min={0} defaultValue={5} /></label><fieldset><legend>{t('channels')}</legend>{data.channels.map((channel) => <label className="check" key={channel.id}><input type="checkbox" name="channel_ids" value={channel.id} />{channel.name}</label>)}</fieldset><button>{t('createRule')}</button></form>
        <div className="list">{data.rules.map((rule) => <article className="list-item" key={rule.id}><div className="item-main"><strong>{rule.name}</strong><span>{metricLabel(rule.metric, t)} · {rule.scopeKind === 'node' ? t('node') : t('group')} {rule.scopeValue} · {rule.enabled ? t('enabled') : t('disabled')}</span></div><div className="button-row"><button onClick={() => void mutate(() => apiMutation(`/api/v1/admin/alert-rules/${encodeURIComponent(rule.id)}`, { method: 'PATCH', body: { enabled: !rule.enabled } }), t(rule.enabled ? 'ruleDisabled' : 'ruleEnabled'))}>{rule.enabled ? t('disable') : t('enable')}</button><button className="danger" onClick={() => { if (window.confirm(t('confirmDeleteRule', { name: rule.name }))) void mutate(() => apiMutation(`/api/v1/admin/alert-rules/${encodeURIComponent(rule.id)}`, { method: 'DELETE' }), t('ruleDeleted')) }}>{t('delete')}</button></div></article>)}</div>
      </section>
    </div>
  )
}

function metricLabel(metric: AlertRule['metric'], t: Translate): string {
  switch (metric) {
    case 'offline': return t('offline')
    case 'cpu': return 'CPU %'
    case 'memory': return t('memoryPercent')
    case 'disk': return t('diskPercent')
    case 'traffic_rate': return t('trafficRate')
  }
}

function StoragePanel({ storage, mutate }: { storage: StorageStats; mutate: Mutate }) {
  const { language, t } = useI18n()
  const [confirmation, setConfirmation] = useState('')
  const action = (path: string, confirm: string, success: string, pages?: number) => {
    if ((confirm === 'clear_history' || confirm === 'reset_all_telemetry') && confirmation !== confirm) return
    void mutate(() => apiMutation(`/api/v1/admin/telemetry/${path}`, { body: { confirm, ...(pages === undefined ? {} : { pages }) } }), success)
  }
  return <section className="panel"><div className="section-heading"><div><h2>telemetry.db</h2><p className="muted">{t('historySeparate')}</p></div><span className={`status ${storage.state.writable ? 'online' : 'offline'}`}>{storage.state.writable ? t('writable') : storage.state.reason || t('unavailable')}</span></div>
    <dl className="metric-grid"><div><dt>{t('mainDatabase')}</dt><dd>{formatBytes(storage.mainBytes, language)}</dd></div><div><dt>WAL</dt><dd>{formatBytes(storage.walBytes, language)}</dd></div><div><dt>SHM</dt><dd>{formatBytes(storage.shmBytes, language)}</dd></div><div><dt>{t('total')}</dt><dd>{formatBytes(storage.totalBytes, language)}</dd></div><div><dt>{t('freeDisk')}</dt><dd>{formatBytes(storage.freeDiskBytes, language)}</dd></div><div><dt>{t('oldestRetained')}</dt><dd>{formatTime(storage.oldestBucket, language)}</dd></div><div><dt>{t('freePages')}</dt><dd>{storage.freelistPages?.toLocaleString(language === 'zh' ? 'zh-CN' : 'en') ?? t('unavailable')}</dd></div><div><dt>{t('droppedBuckets')}</dt><dd>{storage.state.dropped.toLocaleString(language === 'zh' ? 'zh-CN' : 'en')}</dd></div></dl>
    <div className="button-row"><button onClick={() => action('checkpoint', 'checkpoint', t('checkpointComplete'))}>{t('checkpointWal')}</button><button onClick={() => action('incremental-vacuum', 'incremental_vacuum', t('incrementalVacuumComplete'), 1024)}>{t('reclaimPages')}</button><button onClick={() => action('full-vacuum', 'full_vacuum', t('fullVacuumComplete'))}>{t('fullVacuum')}</button></div>
    <div className="danger-zone"><h3>{t('historyCleanup')}</h3><p>{t('cleanupDescription')}</p><label>{t('typeConfirmation')}<span><code>clear_history</code> / <code>reset_all_telemetry</code></span><input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label><div className="button-row"><button className="danger" disabled={confirmation !== 'clear_history'} onClick={() => action('clear-history', 'clear_history', t('historyCleared'))}>{t('clearHistory')}</button><button className="danger" disabled={confirmation !== 'reset_all_telemetry'} onClick={() => action('reset-all', 'reset_all_telemetry', t('telemetryReset'))}>{t('resetTelemetry')}</button></div></div>
  </section>
}

function PublicConfigPanel({ data, mutate }: { data: DashboardData; mutate: Mutate }) {
  const { t } = useI18n()
  const save = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    return mutate(() => apiMutation('/api/v1/admin/public-page', { method: 'PUT', body: {
      enabled: form.get('enabled') === 'on', title: form.get('title'), description: form.get('description'), node_ids: form.getAll('node_ids').map(String),
    } }), t('publicationSaved'))
  }
  return <section className="panel"><h2>{t('publicationAllowlist')}</h2><p className="muted">{t('publicationDescription')}</p><form className="form-grid" key={JSON.stringify(data.publicPage)} onSubmit={(event) => void save(event)}><label className="check"><input name="enabled" type="checkbox" defaultChecked={data.publicPage.enabled} /> {t('enablePublicPage')}</label><label>{t('title')}<input name="title" defaultValue={data.publicPage.title} maxLength={128} required /></label><label className="full">{t('description')}<textarea name="description" defaultValue={data.publicPage.description} maxLength={512} rows={3} /></label><fieldset className="full"><legend>{t('publishedNodes')}</legend>{data.nodes.map((node) => <label className="check" key={node.id}><input name="node_ids" type="checkbox" value={node.id} defaultChecked={data.publicPage.nodeIds.includes(node.id)} />{node.name} <span className="muted">{node.group}</span></label>)}</fieldset><button className="primary">{t('savePublication')}</button></form></section>
}

function SecurityPanel({ session, refreshSession, mutate }: { session: SessionResponse; refreshSession: () => Promise<void>; mutate: Mutate }) {
  const { t } = useI18n()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [recovery, setRecovery] = useState<string[]>([])
  const start = async () => {
    try { setSetup(await apiRequest('/api/v1/admin/mfa/totp/setup', decodeTOTPSetup, { method: 'POST' })) } catch (caught) { window.alert(errorMessage(caught, t)) }
  }
  const confirm = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    try { setRecovery(await apiRequest('/api/v1/admin/mfa/totp/confirm', decodeRecoveryCodes, { method: 'POST', body: { code: form.get('code') } })); setSetup(null); await refreshSession() } catch (caught) { window.alert(errorMessage(caught, t)) }
  }
  const disable = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    return mutate(async () => { await apiMutation('/api/v1/admin/mfa/totp/disable', { body: { password: form.get('password'), code: form.get('code') } }); await refreshSession() }, t('mfaDisabled'))
  }
  return <section className="panel"><h2>{t('multiFactorAuthentication')}</h2><p>{t('statusLabel')} <strong>{session.admin.mfaEnabled ? t('enabled') : t('disabled')}</strong></p>{!session.admin.mfaEnabled && !setup && <button className="primary" onClick={() => void start()}>{t('setupTotp')}</button>}{setup && <div className="secret-once"><p>{t('authenticatorSecret')}</p><code>{setup.secret}</code><p className="word-break">{setup.uri}</p><form onSubmit={(event) => void confirm(event)}><label>{t('sixDigitCode')}<input name="code" inputMode="numeric" autoComplete="one-time-code" required /></label><button>{t('confirmTotp')}</button></form></div>}{recovery.length > 0 && <div className="secret-once"><h3>{t('recoveryCodes')}</h3><ul>{recovery.map((code) => <li key={code}><code>{code}</code></li>)}</ul></div>}{session.admin.mfaEnabled && <form className="form-grid" onSubmit={(event) => void disable(event)}><label>{t('password')}<input name="password" type="password" autoComplete="current-password" /></label><label>{t('mfaOrRecoveryCode')}<input name="code" autoComplete="one-time-code" required /></label><button className="danger">{t('disableMfa')}</button></form>}<p className="muted">{t('mfaRecoveryNotice')}</p></section>
}

export function adminPrivateFieldSummary(node: Node): string {
  return `${node.hostname} ${node.os} ${node.kernel}`
}

export function currentMonthlyTraffic(rows: TrafficTotal[]): TrafficTotal | undefined {
  const month = new Date().toISOString().slice(0, 7)
  return rows.find((row) => row.period === month)
}
