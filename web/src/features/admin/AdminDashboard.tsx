import { type FormEvent, useCallback, useEffect, useState } from 'react'

import { APIError, apiMutation, apiRequest, errorMessage } from '../../api/client'
import {
  decodeChannels, decodeCredential, decodeEnrollment, decodeHostPoints, decodeNodes, decodeProbePoints, decodeProbes,
  decodePublicPageConfig, decodeRecoveryCodes, decodeRules, decodeStorageStats, decodeTOTPSetup, decodeTraffic,
  reduceNodeEvent, type AlertRule, type HostPoint, type Node, type NotificationChannel, type ProbePoint, type ProbeTask,
  type PublicPageConfig, type SessionResponse, type StorageStats, type TrafficTotal,
} from '../../api/contracts'
import { MetricChart } from '../../components/MetricChart'
import { formatBytes, formatDuration, formatPercent, formatRate, formatTime, ratio } from '../../format'
import { useNodeLiveStream } from '../live/useNodeLiveStream'

type Section = 'nodes' | 'probes' | 'alerts' | 'storage' | 'public' | 'security'

interface DashboardData {
  nodes: Node[]
  probes: ProbeTask[]
  channels: NotificationChannel[]
  rules: AlertRule[]
  storage: StorageStats
  publicPage: PublicPageConfig
}

type NodeTelemetryState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; history: HostPoint[]; traffic: TrafficTotal[] }

type ProbeHistoryState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; points: ProbePoint[] }

interface AdminDashboardProps {
  session: SessionResponse
  refreshSession: () => Promise<void>
  logout: () => Promise<void>
  onUnauthorized: () => void
}

export function AdminDashboard({ session, refreshSession, logout, onUnauthorized }: AdminDashboardProps) {
  const [section, setSection] = useState<Section>('nodes')
  const [data, setData] = useState<DashboardData | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [revision, setRevision] = useState(0)

  const load = useCallback(async (signal: AbortSignal) => {
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
      setData({ nodes, probes, channels, rules, storage, publicPage })
      setError('')
    } catch (caught) {
      if (signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) {
        onUnauthorized()
        return
      }
      setError(errorMessage(caught))
    }
  }, [onUnauthorized])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load, revision])

  const onLiveEvent = useCallback((event: Parameters<typeof reduceNodeEvent>[1]) => {
    setData((current) => current ? { ...current, nodes: reduceNodeEvent(current.nodes, event) } : current)
  }, [])
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
      setError(errorMessage(caught))
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="/">VPS <span>Status</span></a>
        <nav aria-label="Administrator sections">
          {(['nodes', 'probes', 'alerts', 'storage', 'public', 'security'] as const).map((item) => (
            <button key={item} className={section === item ? 'active' : ''} aria-current={section === item ? 'page' : undefined} onClick={() => setSection(item)}>
              {sectionLabel(item)}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer"><span>{session.admin.username}</span><button onClick={() => { void logout().catch((caught: unknown) => setError(errorMessage(caught))) }}>Sign out</button></div>
      </aside>
      <main className="content" id="main-content">
        <header className="content-header">
          <div><p className="eyebrow">Control</p><h1>{sectionLabel(section)}</h1></div>
          <a href="/public" target="_blank" rel="noreferrer">Public page ↗</a>
        </header>
        {error && <p className="notice error" role="alert">{error}</p>}
        {notice && <p className="notice success" role="status">{notice}</p>}
        {!data ? <p role="status">Loading administration data…</p> : (
          <>
            {section === 'nodes' && <NodesPanel data={data} mutate={mutate} onUnauthorized={onUnauthorized} />}
            {section === 'probes' && <ProbesPanel data={data} mutate={mutate} onUnauthorized={onUnauthorized} />}
            {section === 'alerts' && <AlertsPanel data={data} mutate={mutate} />}
            {section === 'storage' && <StoragePanel storage={data.storage} mutate={mutate} />}
            {section === 'public' && <PublicConfigPanel data={data} mutate={mutate} />}
            {section === 'security' && <SecurityPanel session={session} refreshSession={refreshSession} mutate={mutate} />}
          </>
        )}
      </main>
    </div>
  )
}

function sectionLabel(section: Section): string {
  switch (section) {
    case 'nodes': return 'Nodes'
    case 'probes': return 'TCP probes'
    case 'alerts': return 'Alerts'
    case 'storage': return 'Telemetry storage'
    case 'public': return 'Public page'
    case 'security': return 'Security'
  }
}

type Mutate = (action: () => Promise<void>, success: string) => Promise<void>

function NodesPanel({ data, mutate, onUnauthorized }: { data: DashboardData; mutate: Mutate; onUnauthorized: () => void }) {
  const [selectedID, setSelectedID] = useState(data.nodes[0]?.id ?? '')
  const [telemetry, setTelemetry] = useState<NodeTelemetryState>({ status: 'loading' })
  const [oneTime, setOneTime] = useState('')
  const selected = data.nodes.find((node) => node.id === selectedID) ?? data.nodes[0]

  useEffect(() => {
    if (!selected) return
    setTelemetry({ status: 'loading' })
    const controller = new AbortController()
    const to = new Date()
    const from = new Date(to.getTime() - 24 * 60 * 60 * 1000)
    const query = new URLSearchParams({ node_id: selected.id, resolution: '1m', from: from.toISOString(), to: to.toISOString() })
    Promise.all([
      apiRequest(`/api/v1/admin/telemetry/host?${query}`, decodeHostPoints, { signal: controller.signal }),
      apiRequest(`/api/v1/admin/telemetry/traffic?node_id=${encodeURIComponent(selected.id)}`, decodeTraffic, { signal: controller.signal }),
    ]).then(([points, totals]) => {
      setTelemetry({ status: 'success', history: points, traffic: totals })
    }).catch((caught: unknown) => {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) {
        onUnauthorized()
        return
      }
      setTelemetry({ status: 'error', message: errorMessage(caught) })
    })
    return () => controller.abort()
  }, [selected?.id, onUnauthorized])

  const enroll = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    try {
      const enrollment = await apiRequest('/api/v1/admin/enrollments', decodeEnrollment, {
        method: 'POST', body: { node_name: form.get('node_name'), control_url: form.get('control_url'), expires_in_seconds: 3600 },
      })
      setOneTime(`${enrollment.installCommand}\n\nToken (shown once): ${enrollment.enrollmentToken}\nExpires: ${formatTime(enrollment.expiresAt)}`)
    } catch (caught) {
      setOneTime(errorMessage(caught))
    }
  }

  return (
    <div className="stack">
      <section className="panel">
        <div className="section-heading"><div><h2>Fleet overview</h2><p className="muted">Live values arrive over an authenticated SSE stream.</p></div><span>{data.nodes.filter((node) => node.online).length}/{data.nodes.length} online</span></div>
        <div className="node-grid">
          {data.nodes.length === 0 ? <p className="empty">No nodes enrolled yet.</p> : data.nodes.map((node) => (
            <button key={node.id} className={`node-card ${selected?.id === node.id ? 'selected' : ''}`} onClick={() => setSelectedID(node.id)}>
              <span className={`status ${node.online ? 'online' : 'offline'}`}>{node.online ? 'Online' : node.connected ? 'Stale' : 'Offline'}</span>
              <strong>{node.name}</strong><span>{node.group || 'Ungrouped'}</span>
              <span>CPU {formatPercent(node.report?.cpuUsagePercent)}</span>
              <span>↓ {formatRate(node.report?.receiveBytesPerSecond)} · ↑ {formatRate(node.report?.transmitBytesPerSecond)}</span>
            </button>
          ))}
        </div>
      </section>
      {selected && <NodeDetail node={selected} telemetry={telemetry} mutate={mutate} setOneTime={setOneTime} />}
      <section className="panel">
        <h2>Enroll a Linux node</h2>
        <form className="form-grid" onSubmit={(event) => void enroll(event)}>
          <label>Display name<input name="node_name" maxLength={128} required /></label>
          <label>Control URL<input name="control_url" type="url" placeholder="https://status.example.com" required /></label>
          <button className="primary">Create one-time install command</button>
        </form>
        {oneTime && <pre className="secret-once" role="status">{oneTime}</pre>}
      </section>
    </div>
  )
}

function NodeDetail({ node, telemetry, mutate, setOneTime }: {
  node: Node; telemetry: NodeTelemetryState; mutate: Mutate; setOneTime: (value: string) => void
}) {
  const month = telemetry.status === 'success'
    ? telemetry.traffic.find((row) => row.period === new Date().toISOString().slice(0, 7))
    : undefined
  const update = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    return mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}`, {
      method: 'PATCH', body: { name: form.get('name'), group: form.get('group'), hidden: form.get('hidden') === 'on' },
    }), 'Node metadata updated.')
  }
  const rotate = async () => {
    try {
      const result = await apiRequest(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}/credentials/rotate`, decodeCredential, { method: 'POST' })
      setOneTime(`New node credential (shown once): ${result.credential}`)
    } catch (caught) {
      setOneTime(errorMessage(caught))
    }
  }
  return (
    <section className="panel">
      <div className="section-heading"><div><h2>{node.name}</h2><p className="muted">Last report {formatTime(node.lastReportAt)}</p></div><span className={`status ${node.online ? 'online' : 'offline'}`}>{node.online ? 'Online' : 'Offline'}</span></div>
      <dl className="metric-grid">
        <div><dt>CPU</dt><dd>{formatPercent(node.report?.cpuUsagePercent)}</dd></div>
        <div><dt>Load (1m)</dt><dd>{node.report?.loadOne?.toFixed(2) ?? 'Unavailable'}</dd></div>
        <div><dt>Memory</dt><dd>{formatPercent(ratio(node.report?.memoryUsedBytes, node.report?.memoryTotalBytes))}</dd></div>
        <div><dt>Disk</dt><dd>{formatPercent(ratio(node.report?.diskUsedBytes, node.report?.diskTotalBytes))}</dd></div>
        <div><dt>Uptime</dt><dd>{formatDuration(node.report?.uptimeSeconds)}</dd></div>
        <div><dt>Processes</dt><dd>{node.report?.processCount ?? 'Unavailable'}</dd></div>
        <div><dt>Month received</dt><dd>{formatBytes(month?.receiveBytes)}</dd></div>
        <div><dt>Month sent</dt><dd>{formatBytes(month?.transmitBytes)}</dd></div>
      </dl>
      <details><summary>Host identity (administrator only)</summary><dl className="detail-list"><div><dt>Hostname</dt><dd>{node.hostname || 'Unknown'}</dd></div><div><dt>OS / kernel</dt><dd>{node.os || 'Unknown'} · {node.kernel || 'Unknown'}</dd></div><div><dt>Architecture</dt><dd>{node.architecture || 'Unknown'} · {node.virtualization || 'Unknown'}</dd></div></dl></details>
      {telemetry.status === 'loading' ? <p role="status">Loading history and UTC-month traffic…</p> : telemetry.status === 'error' ? (
        <p className="notice warning" role="alert">History unavailable: {telemetry.message}</p>
      ) : (
        <div className="chart-grid">
          <MetricChart title="CPU history" points={telemetry.history.map((point) => ({ at: point.bucketStart, value: point.cpuAverage }))} formatValue={formatPercent} />
          <MetricChart title="Network receive history" points={telemetry.history.map((point) => ({ at: point.bucketStart, value: point.receiveRateAverage }))} formatValue={formatRate} />
        </div>
      )}
      <form className="form-grid compact" onSubmit={(event) => void update(event)}>
        <label>Display name<input name="name" defaultValue={node.name} maxLength={128} required /></label>
        <label>Group / region<input name="group" defaultValue={node.group} maxLength={128} /></label>
        <label className="check"><input name="hidden" type="checkbox" defaultChecked={node.hidden} /> Hide in administrator overview</label>
        <button>Save node</button>
      </form>
      <div className="button-row">
        <button onClick={() => void rotate()}>Rotate credential</button>
        <button onClick={() => { if (window.confirm(`Revoke every credential for ${node.name}? The Agent will disconnect immediately.`)) void mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}/credentials`, { method: 'DELETE' }), 'Node credentials revoked.') }}>Revoke credentials</button>
        <button className="danger" onClick={() => { if (window.confirm(`Delete ${node.name}? Historical cleanup will be queued.`)) void mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}`, { method: 'DELETE' }), 'Node deleted.') }}>Delete node</button>
      </div>
    </section>
  )
}

function ProbesPanel({ data, mutate, onUnauthorized }: { data: DashboardData; mutate: Mutate; onUnauthorized: () => void }) {
  const [selectedID, setSelectedID] = useState(data.probes[0]?.id ?? '')
  const [history, setHistory] = useState<ProbeHistoryState>({ status: 'idle' })
  const selected = data.probes.find((probe) => probe.id === selectedID) ?? data.probes[0]
  const selectedNode = selected?.nodeIds[0]
  useEffect(() => {
    if (!selected || !selectedNode) { setHistory({ status: 'idle' }); return }
    setHistory({ status: 'loading' })
    const controller = new AbortController()
    const to = new Date(), from = new Date(to.getTime() - 24 * 60 * 60 * 1000)
    const query = new URLSearchParams({ node_id: selectedNode, task_id: selected.id, resolution: '1m', from: from.toISOString(), to: to.toISOString() })
    apiRequest(`/api/v1/admin/telemetry/probe?${query}`, decodeProbePoints, { signal: controller.signal })
      .then((points) => setHistory({ status: 'success', points }))
      .catch((caught: unknown) => {
        if (controller.signal.aborted) return
        if (caught instanceof APIError && caught.status === 401) {
          onUnauthorized()
          return
        }
        setHistory({ status: 'error', message: errorMessage(caught) })
      })
    return () => controller.abort()
  }, [selected?.id, selectedNode, onUnauthorized])
  const create = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    return mutate(() => apiMutation('/api/v1/admin/probes', { body: {
      name: form.get('name'), host: form.get('host'), port: Number(form.get('port')),
      interval_seconds: Number(form.get('interval')), node_ids: form.getAll('node_ids').map(String),
    } }), 'TCP probe created.')
  }
  return (
    <div className="stack">
      <section className="panel"><h2>Create connect-only TCP probe</h2><p className="muted">5–3600 second interval; the Agent connects and immediately closes without payload or banner reads.</p>
        <form className="form-grid" onSubmit={(event) => void create(event)}>
          <label>Name<input name="name" maxLength={128} required /></label><label>Host<input name="host" required /></label>
          <label>Port<input name="port" type="number" min={1} max={65535} required /></label><label>Interval (seconds)<input name="interval" type="number" min={5} max={3600} defaultValue={30} required /></label>
          <fieldset><legend>Execution nodes (maximum 64 tasks per node)</legend>{data.nodes.map((node) => <label className="check" key={node.id}><input type="checkbox" name="node_ids" value={node.id} />{node.name}</label>)}</fieldset>
          <button className="primary">Create probe</button>
        </form>
      </section>
      <section className="panel"><h2>Probe tasks</h2>{data.probes.length === 0 ? <p className="empty">No TCP probes configured.</p> : <div className="list">{data.probes.map((probe) => {
        const attempts = probe.latest.length, failures = probe.latest.filter((item) => !item.success).length
        return <article key={probe.id} className={selected?.id === probe.id ? 'list-item selected' : 'list-item'}><button className="item-main" onClick={() => setSelectedID(probe.id)}><strong>{probe.name}</strong><span>{probe.host}:{probe.port} · every {probe.intervalSeconds}s</span><span>Current failure rate {attempts ? formatPercent((failures / attempts) * 100) : 'No results'}</span></button><button className="danger" aria-label={`Delete ${probe.name}`} onClick={() => { if (window.confirm(`Delete TCP probe ${probe.name}? Its retained history will be cleaned up.`)) void mutate(() => apiMutation(`/api/v1/admin/probes/${encodeURIComponent(probe.id)}`, { method: 'DELETE' }), 'Probe deleted.') }}>Delete</button></article>
      })}</div>}</section>
      {selected && <section className="panel"><h2>{selected.name} history</h2>{history.status === 'loading' ? <p role="status">Loading probe history…</p> : history.status === 'error' ? <p className="notice warning" role="alert">Probe history unavailable: {history.message}</p> : history.status === 'idle' ? <p className="empty">Assign this probe to a node to collect history.</p> : <div className="chart-grid"><MetricChart title="TCP latency" points={history.points.map((point) => ({ at: point.bucketStart, value: point.latencyAverage }))} formatValue={(value) => value === null ? 'Unavailable' : `${value.toFixed(1)} ms`} /><MetricChart title="TCP failure rate" points={history.points.map((point) => ({ at: point.bucketStart, value: (1 - point.successRate) * 100 }))} formatValue={formatPercent} /></div>}</section>}
    </div>
  )
}

function AlertsPanel({ data, mutate }: { data: DashboardData; mutate: Mutate }) {
  const createChannel = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget); const kind = String(form.get('kind'))
    const body = kind === 'webhook'
      ? { name: form.get('name'), kind, enabled: true, url: form.get('url'), secret: form.get('secret') }
      : { name: form.get('name'), kind, enabled: true, bot_token: form.get('bot_token'), chat_id: form.get('chat_id') }
    return mutate(() => apiMutation('/api/v1/admin/notification-channels', { body }), 'Notification channel created.')
  }
  const createRule = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget); const metric = String(form.get('metric'))
    return mutate(() => apiMutation('/api/v1/admin/alert-rules', { body: {
      name: form.get('name'), scope_kind: form.get('scope_kind'), scope_value: form.get('scope_value'), metric,
      threshold: metric === 'offline' ? 0 : Number(form.get('threshold')), duration_seconds: Number(form.get('duration')),
      recovery_hysteresis: metric === 'offline' ? 0 : Number(form.get('hysteresis')), enabled: true,
      reminder_interval_seconds: 0, reminder_max_count: 0, channel_ids: form.getAll('channel_ids').map(String),
    } }), 'Alert rule created.')
  }
  return (
    <div className="two-column">
      <section className="panel"><h2>Notification channels</h2><p className="muted">Secrets are write-only and encrypted at rest.</p>
        <form className="form-grid" onSubmit={(event) => void createChannel(event)}><label>Name<input name="name" required /></label><label>Kind<select name="kind" defaultValue="webhook"><option value="webhook">Webhook</option><option value="telegram">Telegram</option></select></label><label>Webhook URL<input name="url" type="url" placeholder="https://…" /></label><label>Webhook HMAC secret<input name="secret" type="password" /></label><label>Telegram bot token<input name="bot_token" type="password" /></label><label>Telegram chat ID<input name="chat_id" type="password" /></label><button>Create channel</button></form>
        <div className="list">{data.channels.map((channel) => <article className="list-item" key={channel.id}><div className="item-main"><strong>{channel.name}</strong><span>{channel.kind} · {channel.configured ? 'Configured' : 'Incomplete'} · {channel.enabled ? 'Enabled' : 'Disabled'}</span></div><div className="button-row"><button onClick={() => void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}`, { method: 'PATCH', body: { enabled: !channel.enabled } }), `Channel ${channel.enabled ? 'disabled' : 'enabled'}.`)}>{channel.enabled ? 'Disable' : 'Enable'}</button><button disabled={!channel.enabled} onClick={() => void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}/test`), 'Test notification queued.')}>Test</button><button className="danger" onClick={() => { if (window.confirm(`Delete notification channel ${channel.name}? It will be removed from every alert rule.`)) void mutate(() => apiMutation(`/api/v1/admin/notification-channels/${encodeURIComponent(channel.id)}`, { method: 'DELETE' }), 'Channel deleted.') }}>Delete</button></div></article>)}</div>
      </section>
      <section className="panel"><h2>Alert rules</h2><form className="form-grid" onSubmit={(event) => void createRule(event)}><label>Name<input name="name" required /></label><label>Scope<select name="scope_kind"><option value="node">Node</option><option value="group">Group</option></select></label><label>Node ID or group<input name="scope_value" list="alert-scopes" required /><datalist id="alert-scopes">{data.nodes.flatMap((node) => [<option key={node.id} value={node.id}>{node.name}</option>, <option key={`${node.id}-group`} value={node.group}>{node.group}</option>])}</datalist></label><label>Metric<select name="metric"><option value="offline">Offline</option><option value="cpu">CPU %</option><option value="memory">Memory %</option><option value="disk">Disk %</option><option value="traffic_rate">Traffic bytes/s</option></select></label><label>Threshold<input name="threshold" type="number" min={0} defaultValue={90} /></label><label>Duration (seconds)<input name="duration" type="number" min={0} max={86400} defaultValue={60} /></label><label>Recovery hysteresis<input name="hysteresis" type="number" min={0} defaultValue={5} /></label><fieldset><legend>Channels</legend>{data.channels.map((channel) => <label className="check" key={channel.id}><input type="checkbox" name="channel_ids" value={channel.id} />{channel.name}</label>)}</fieldset><button>Create rule</button></form>
        <div className="list">{data.rules.map((rule) => <article className="list-item" key={rule.id}><div className="item-main"><strong>{rule.name}</strong><span>{rule.metric} · {rule.scopeKind} {rule.scopeValue} · {rule.enabled ? 'Enabled' : 'Disabled'}</span></div><div className="button-row"><button onClick={() => void mutate(() => apiMutation(`/api/v1/admin/alert-rules/${encodeURIComponent(rule.id)}`, { method: 'PATCH', body: { enabled: !rule.enabled } }), `Rule ${rule.enabled ? 'disabled' : 'enabled'}.`)}>{rule.enabled ? 'Disable' : 'Enable'}</button><button className="danger" onClick={() => { if (window.confirm(`Delete alert rule ${rule.name}? Its current state and pending deliveries will be removed.`)) void mutate(() => apiMutation(`/api/v1/admin/alert-rules/${encodeURIComponent(rule.id)}`, { method: 'DELETE' }), 'Rule deleted.') }}>Delete</button></div></article>)}</div>
      </section>
    </div>
  )
}

function StoragePanel({ storage, mutate }: { storage: StorageStats; mutate: Mutate }) {
  const [confirmation, setConfirmation] = useState('')
  const action = (path: string, confirm: string, success: string, pages?: number) => {
    if ((confirm === 'clear_history' || confirm === 'reset_all_telemetry') && confirmation !== confirm) return
    void mutate(() => apiMutation(`/api/v1/admin/telemetry/${path}`, { body: { confirm, ...(pages === undefined ? {} : { pages }) } }), success)
  }
  return <section className="panel"><div className="section-heading"><div><h2>telemetry.db</h2><p className="muted">History is physically separate from control.db.</p></div><span className={`status ${storage.state.writable ? 'online' : 'offline'}`}>{storage.state.writable ? 'Writable' : storage.state.reason || 'Unavailable'}</span></div>
    <dl className="metric-grid"><div><dt>Main</dt><dd>{formatBytes(storage.mainBytes)}</dd></div><div><dt>WAL</dt><dd>{formatBytes(storage.walBytes)}</dd></div><div><dt>SHM</dt><dd>{formatBytes(storage.shmBytes)}</dd></div><div><dt>Total</dt><dd>{formatBytes(storage.totalBytes)}</dd></div><div><dt>Free disk</dt><dd>{formatBytes(storage.freeDiskBytes)}</dd></div><div><dt>Oldest retained</dt><dd>{formatTime(storage.oldestBucket)}</dd></div><div><dt>Free pages</dt><dd>{storage.freelistPages?.toLocaleString() ?? 'Unavailable'}</dd></div><div><dt>Dropped buckets</dt><dd>{storage.state.dropped.toLocaleString()}</dd></div></dl>
    <div className="button-row"><button onClick={() => action('checkpoint', 'checkpoint', 'WAL checkpoint complete.')}>Checkpoint WAL</button><button onClick={() => action('incremental-vacuum', 'incremental_vacuum', 'Incremental vacuum complete.', 1024)}>Reclaim 1,024 pages</button><button onClick={() => action('full-vacuum', 'full_vacuum', 'Full vacuum complete.')}>Full vacuum</button></div>
    <div className="danger-zone"><h3>History cleanup</h3><p>Clear history preserves lifetime and monthly traffic. Reset all telemetry also removes traffic totals.</p><label>Type <code>clear_history</code> or <code>reset_all_telemetry</code><input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label><div className="button-row"><button className="danger" disabled={confirmation !== 'clear_history'} onClick={() => action('clear-history', 'clear_history', 'History cleared; traffic preserved.')}>Clear history</button><button className="danger" disabled={confirmation !== 'reset_all_telemetry'} onClick={() => action('reset-all', 'reset_all_telemetry', 'All telemetry reset.')}>Reset all telemetry</button></div></div>
  </section>
}

function PublicConfigPanel({ data, mutate }: { data: DashboardData; mutate: Mutate }) {
  const save = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    return mutate(() => apiMutation('/api/v1/admin/public-page', { method: 'PUT', body: {
      enabled: form.get('enabled') === 'on', title: form.get('title'), description: form.get('description'), node_ids: form.getAll('node_ids').map(String),
    } }), 'Public page configuration saved.')
  }
  return <section className="panel"><h2>Server-side publication allowlist</h2><p className="muted">Every node is private by default. The public API uses a separate DTO and never exposes host identity or history.</p><form className="form-grid" key={JSON.stringify(data.publicPage)} onSubmit={(event) => void save(event)}><label className="check"><input name="enabled" type="checkbox" defaultChecked={data.publicPage.enabled} /> Enable public page</label><label>Title<input name="title" defaultValue={data.publicPage.title} maxLength={128} required /></label><label className="full">Description<textarea name="description" defaultValue={data.publicPage.description} maxLength={512} rows={3} /></label><fieldset className="full"><legend>Published nodes</legend>{data.nodes.map((node) => <label className="check" key={node.id}><input name="node_ids" type="checkbox" value={node.id} defaultChecked={data.publicPage.nodeIds.includes(node.id)} />{node.name} <span className="muted">{node.group}</span></label>)}</fieldset><button className="primary">Save publication settings</button></form></section>
}

function SecurityPanel({ session, refreshSession, mutate }: { session: SessionResponse; refreshSession: () => Promise<void>; mutate: Mutate }) {
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [recovery, setRecovery] = useState<string[]>([])
  const start = async () => {
    try { setSetup(await apiRequest('/api/v1/admin/mfa/totp/setup', decodeTOTPSetup, { method: 'POST' })) } catch (caught) { window.alert(errorMessage(caught)) }
  }
  const confirm = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    try { setRecovery(await apiRequest('/api/v1/admin/mfa/totp/confirm', decodeRecoveryCodes, { method: 'POST', body: { code: form.get('code') } })); setSetup(null); await refreshSession() } catch (caught) { window.alert(errorMessage(caught)) }
  }
  const disable = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); const form = new FormData(event.currentTarget)
    return mutate(async () => { await apiMutation('/api/v1/admin/mfa/totp/disable', { body: { password: form.get('password'), code: form.get('code') } }); await refreshSession() }, 'MFA disabled.')
  }
  return <section className="panel"><h2>Multi-factor authentication</h2><p>Status: <strong>{session.admin.mfaEnabled ? 'Enabled' : 'Disabled'}</strong></p>{!session.admin.mfaEnabled && !setup && <button className="primary" onClick={() => void start()}>Set up TOTP</button>}{setup && <div className="secret-once"><p>Add this secret to your authenticator. It is shown only during setup.</p><code>{setup.secret}</code><p className="word-break">{setup.uri}</p><form onSubmit={(event) => void confirm(event)}><label>Six-digit code<input name="code" inputMode="numeric" autoComplete="one-time-code" required /></label><button>Confirm TOTP</button></form></div>}{recovery.length > 0 && <div className="secret-once"><h3>Recovery codes — save now</h3><ul>{recovery.map((code) => <li key={code}><code>{code}</code></li>)}</ul></div>}{session.admin.mfaEnabled && <form className="form-grid" onSubmit={(event) => void disable(event)}><label>Password<input name="password" type="password" autoComplete="current-password" required /></label><label>TOTP or recovery code<input name="code" autoComplete="one-time-code" required /></label><button className="danger">Disable MFA</button></form>}<p className="muted">If both TOTP and recovery codes are lost, MFA can only be reset through the local Control CLI with direct data-directory access.</p></section>
}

export function adminPrivateFieldSummary(node: Node): string {
  return `${node.hostname} ${node.os} ${node.kernel}`
}

export function currentMonthlyTraffic(rows: TrafficTotal[]): TrafficTotal | undefined {
  const month = new Date().toISOString().slice(0, 7)
  return rows.find((row) => row.period === month)
}
