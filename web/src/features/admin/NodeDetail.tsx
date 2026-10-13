import { type FormEvent } from 'react'

import { APIError, apiMutation, apiRequest, errorMessage } from '../../api/client'
import { decodeCredential, type Node } from '../../api/contracts'
import { MetricChart } from '../../components/MetricChart'
import { ResourceMeters, StatusBadge } from '../../components/NodeMetrics'
import { formatBytes, formatDuration, formatPercent, formatRate, formatTime } from '../../format'
import { useI18n } from '../../i18n'
import { type Mutate, type NodeTelemetryState } from './NodesPanel'

export function NodeDetail({ node, telemetry, mutate, setOneTime, onUnauthorized }: {
  node: Node; telemetry: NodeTelemetryState; mutate: Mutate; setOneTime: (value: string) => void; onUnauthorized: () => void
}) {
  const { language, t } = useI18n()
  const month = telemetry.status === 'success'
    ? telemetry.traffic.find((row) => row.period === new Date().toISOString().slice(0, 7))
    : undefined
  const update = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    return mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}`, {
      method: 'PATCH', body: { name: form.get('name'), group: form.get('group'), hidden: form.get('hidden') === 'on' },
    }), t('nodeUpdated'))
  }
  const rotate = async () => {
    try {
      const result = await apiRequest(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}/credentials/rotate`, decodeCredential, { method: 'POST' })
      setOneTime(t('credentialShownOnce', { credential: result.credential }))
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
      setOneTime(errorMessage(caught, t))
    }
  }
  return (
    <section className="panel node-detail" aria-label={node.name}>
      <div className="section-heading"><div><h2>{node.name}</h2><p className="muted">{t('lastReport', { time: formatTime(node.lastReportAt, language) })}</p></div><StatusBadge online={node.online} stale={node.connected && !node.online} /></div>
      <h3>{t('resourceUsage')}</h3><ResourceMeters values={node.online ? node.report : null} />
      {!node.online && <p className="muted">{t('offlineMetrics')}</p>}
      <dl className="metric-grid">
        <div><dt>{t('loadOne')}</dt><dd>{node.online ? node.report?.loadOne?.toFixed(2) ?? t('unavailable') : t('unavailable')}</dd></div>
        <div><dt>{t('uptime')}</dt><dd>{formatDuration(node.online ? node.report?.uptimeSeconds : null, language)}</dd></div>
        <div><dt>{t('processes')}</dt><dd>{node.online ? node.report?.processCount ?? t('unavailable') : t('unavailable')}</dd></div>
        <div><dt>{t('swap')}</dt><dd>{formatBytes(node.online ? node.report?.swapUsedBytes : null, language)} / {formatBytes(node.online ? node.report?.swapTotalBytes : null, language)}</dd></div>
        <div><dt>{t('connectionCounts')}</dt><dd>{node.online ? `${node.report?.tcpConnections ?? '—'} / ${node.report?.udpConnections ?? '—'}` : t('unavailable')}</dd></div>
      </dl>
      <h3>{t('networkTraffic')}</h3><dl className="metric-grid">
        <div><dt>{t('download')}</dt><dd>{formatRate(node.online ? node.report?.receiveBytesPerSecond : null, language)}</dd></div>
        <div><dt>{t('upload')}</dt><dd>{formatRate(node.online ? node.report?.transmitBytesPerSecond : null, language)}</dd></div>
        <div><dt>{t('monthReceived')}</dt><dd>{formatBytes(month?.receiveBytes, language)}</dd></div>
        <div><dt>{t('monthSent')}</dt><dd>{formatBytes(month?.transmitBytes, language)}</dd></div>
      </dl>
      <details open><summary>{t('hostIdentity')}</summary><dl className="detail-list"><div><dt>{t('hostname')}</dt><dd>{node.hostname || t('unknown')}</dd></div><div><dt>{t('osKernel')}</dt><dd>{node.os || t('unknown')} · {node.kernel || t('unknown')}</dd></div><div><dt>{t('architecture')}</dt><dd>{node.architecture || t('unknown')} · {node.virtualization || t('unknown')}</dd></div></dl></details>
      <h3>{t('historyTrends')}</h3>
      {telemetry.status === 'loading' ? <p role="status">{t('loadingHistory')}</p> : telemetry.status === 'error' ? (
        <p className="notice warning" role="alert">{t('historyUnavailable', { message: telemetry.message })}</p>
      ) : (
        <div className="chart-grid">
          <MetricChart title={t('cpuHistory')} points={telemetry.history.map((point) => ({ at: point.bucketStart, value: point.cpuAverage }))} formatValue={(value) => formatPercent(value, language)} />
          <MetricChart title={t('networkReceiveHistory')} points={telemetry.history.map((point) => ({ at: point.bucketStart, value: point.receiveRateAverage }))} formatValue={(value) => formatRate(value, language)} />
        </div>
      )}
      <details className="node-settings"><summary>{t('nodeSettings')}</summary><form className="form-grid compact" onSubmit={(event) => void update(event)}>
        <label>{t('displayName')}<input name="name" defaultValue={node.name} maxLength={128} required /></label>
        <label>{t('groupRegion')}<input name="group" defaultValue={node.group} maxLength={128} /></label>
        <label className="check"><input name="hidden" type="checkbox" defaultChecked={node.hidden} /> {t('hideAdminOverview')}</label>
        <button>{t('saveNode')}</button>
      </form>
      <div className="button-row">
        <button onClick={() => void rotate()}>{t('rotateCredential')}</button>
        <button onClick={() => { if (window.confirm(t('confirmRevokeCredentials', { name: node.name }))) void mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}/credentials`, { method: 'DELETE' }), t('credentialsRevoked')) }}>{t('revokeCredentials')}</button>
        <button className="danger" onClick={() => { if (window.confirm(t('confirmDeleteNode', { name: node.name }))) void mutate(() => apiMutation(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}`, { method: 'DELETE' }), t('nodeDeleted')) }}>{t('deleteNode')}</button>
      </div></details>
    </section>
  )
}
