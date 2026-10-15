import { type RefObject, useEffect, useRef, useState } from 'react'

import { APIError, errorMessage } from '../../api/client'
import type { Node, ProbePoint, ProbeTask } from '../../api/contracts'
import { MetricChart } from '../../components/MetricChart'
import { formatPercent } from '../../format'
import { useI18n, type TranslationKey } from '../../i18n'
import { ProbeResult } from './ProbeResult'
import { SettingsDialog } from './SettingsDialog'
import { loadProbeHistory } from './probeHistory'
import { probeHistoryRanges, probeTarget, type ProbeHistoryRange } from './probePresentation'

type HistoryState = { status: 'loading' } | { status: 'error'; error: unknown } | { status: 'success'; points: ProbePoint[] }
const rangeLabels: Record<ProbeHistoryRange, TranslationKey> = { '24h': 'probeRange24h', '7d': 'probeRange7d', '30d': 'probeRange30d' }

export function ProbeDetailDialog({ task, tasks, nodes, nodeId, onSelect, onClose, onRefresh, revision, onUnauthorized, fallbackFocus }: {
  task: ProbeTask; tasks: ProbeTask[]; nodes: Node[]; nodeId: string
  onSelect: (taskId: string, nodeId: string) => void; onClose: () => void; onRefresh: () => void
  revision: number; onUnauthorized: () => void; fallbackFocus?: RefObject<HTMLElement | null>
}) {
  const { t } = useI18n()
  const [range, setRange] = useState<ProbeHistoryRange>('24h')
  const nodeName = (id: string) => nodes.find((node) => node.id === id)?.name ?? id
  return <SettingsDialog title={t('probeDetailsTitle', { name: task.name })} pending={false} onClose={onClose} className="probe-detail-dialog" fallbackFocus={fallbackFocus}>
    <dl className="probe-detail-meta">
      <div><dt>{t('name')}</dt><dd>{task.name}</dd></div>
      <div><dt>{t('probeTargetLabel')}</dt><dd>{probeTarget(task)}</dd></div>
      <div><dt>{t('probeMethodLabel')}</dt><dd>{t('probeTcpMethod')}</dd></div>
      <div><dt>{t('intervalSeconds')}</dt><dd>{t('everySeconds', { seconds: task.intervalSeconds })}</dd></div>
    </dl>
    <section className="probe-detail-results" aria-label={t('probeRecentResults')}>
      <div className="section-heading"><h3>{t('probeRecentResults')}</h3><button type="button" onClick={onRefresh}>{t('refreshProbes')}</button></div>
      {task.nodeIds.length === 0 ? <p className="empty">{t('probeUnassigned')}</p> : <ul>{task.nodeIds.map((id) => <li key={id}><button type="button" className="probe-result-link" aria-pressed={nodeId === id} onClick={() => onSelect(task.id, id)}><span>{nodeName(id)}</span><ProbeResult task={task} nodeId={id} /></button></li>)}</ul>}
    </section>
    <section className="probe-detail-history" aria-label={t('historyTrends')}>
      <h3>{t('historyTrends')}</h3>
      <div className="probe-history-controls">
        <label>{t('probeHistoryTask')}<select value={task.id} onChange={(event) => onSelect(event.target.value, '')}>{tasks.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <label>{t('probeHistoryNode')}<select value={nodeId} disabled={task.nodeIds.length === 0} onChange={(event) => onSelect(task.id, event.target.value)}><option value="">{t('probeChooseNode')}</option>{task.nodeIds.map((id) => <option key={id} value={id}>{nodeName(id)}</option>)}</select></label>
        <label>{t('probeHistoryRange')}<select value={range} onChange={(event) => { const value = event.target.value; if (value in probeHistoryRanges) setRange(value as ProbeHistoryRange) }}>{(Object.keys(probeHistoryRanges) as ProbeHistoryRange[]).map((value) => <option key={value} value={value}>{t(rangeLabels[value])}</option>)}</select></label>
      </div>
      <p className="muted probe-history-note">{t('probeRetainedHistoryHint')}</p>
      {task.nodeIds.length === 0 ? <p className="empty">{t('assignProbe')}</p> : !nodeId ? <p className="empty">{t('probeChooseNodeHint')}</p> : <ProbeHistory key={`${task.id}:${task.updatedAt}:${nodeId}:${range}:${revision}`} taskId={task.id} nodeId={nodeId} range={range} onUnauthorized={onUnauthorized} />}
    </section>
    <div className="dialog-actions"><button type="button" onClick={onClose}>{t('close')}</button></div>
  </SettingsDialog>
}

function ProbeHistory({ taskId, nodeId, range, onUnauthorized }: {
  taskId: string; nodeId: string; range: ProbeHistoryRange; onUnauthorized: () => void
}) {
  const { language, t } = useI18n()
  const [state, setState] = useState<HistoryState>({ status: 'loading' })
  const [revision, setRevision] = useState(0)
  const unauthorized = useRef(onUnauthorized)
  useEffect(() => { unauthorized.current = onUnauthorized }, [onUnauthorized])
  useEffect(() => {
    const controller = new AbortController()
    void loadProbeHistory(taskId, nodeId, range, controller.signal).then((points) => {
      if (!controller.signal.aborted) setState({ status: 'success', points })
    }).catch((caught: unknown) => {
      if (controller.signal.aborted) return
      if (caught instanceof APIError && caught.status === 401) { unauthorized.current(); return }
      setState({ status: 'error', error: caught })
    })
    return () => controller.abort()
  }, [taskId, nodeId, range, revision])
  if (state.status === 'loading') return <p role="status">{t('loadingProbeHistory')}</p>
  if (state.status === 'error') return <div><p className="notice warning" role="alert">{t('probeHistoryUnavailable', { message: errorMessage(state.error, t) })}</p><button type="button" onClick={() => { setState({ status: 'loading' }); setRevision((value) => value + 1) }}>{t('retryProbeHistory')}</button></div>
  if (state.points.length === 0) return <p className="empty">{t('probeNoHistory')}</p>
  return <div className="chart-grid"><MetricChart timeScale title={t('tcpLatency')} points={state.points.map((point) => ({ at: point.bucketStart, value: point.latencyAverage }))} formatValue={(value) => value === null ? t('unavailable') : `${value.toFixed(1)} ms`} /><MetricChart timeScale title={t('tcpFailureRate')} points={state.points.map((point) => ({ at: point.bucketStart, value: point.attempts > 0 ? (1 - point.successRate) * 100 : null }))} formatValue={(value) => formatPercent(value, language)} /></div>
}
