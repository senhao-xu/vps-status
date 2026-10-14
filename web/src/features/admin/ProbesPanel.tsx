import { useEffect, useRef, useState } from 'react'

import { APIError, errorMessage } from '../../api/client'
import { type Node, type ProbePoint, type ProbeTask } from '../../api/contracts'
import { MetricChart } from '../../components/MetricChart'
import { StatusBadge } from '../../components/NodeMetrics'
import { formatPercent, formatTime } from '../../format'
import { useI18n, type TranslationKey } from '../../i18n'
import { DeleteProbeDialog, ProbeTaskDialog } from './ProbeTaskDialog'
import { loadProbeHistory } from './probeHistory'
import { latestProbeResult, matchesProbe, probeHistoryRanges, probeSummary, probeTarget, type ProbeHistoryRange } from './probePresentation'

type HistoryState = { status: 'loading' } | { status: 'error'; error: unknown } | { status: 'success'; points: ProbePoint[] }
const rangeLabels: Record<ProbeHistoryRange, TranslationKey> = { '24h': 'probeRange24h', '7d': 'probeRange7d', '30d': 'probeRange30d' }

export function ProbesPanel({ probes, nodes, refresh, onUnauthorized }: {
  probes: ProbeTask[]; nodes: Node[]; refresh: () => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [view, setView] = useState<'task' | 'node'>('task')
  const [query, setQuery] = useState('')
  const [selectedID, setSelectedID] = useState(probes[0]?.id ?? '')
  const [nodeSelection, setNodeSelection] = useState({ taskId: '', nodeId: '' })
  const [range, setRange] = useState<ProbeHistoryRange>('24h')
  const [historyRevision, setHistoryRevision] = useState(0)
  const [editor, setEditor] = useState<{ task: ProbeTask | null } | null>(null)
  const [deleting, setDeleting] = useState<ProbeTask | null>(null)
  const [notice, setNotice] = useState<TranslationKey | null>(null)
  const selected = probes.find((task) => task.id === selectedID) ?? probes[0]
  const selectedNode = selected?.id === nodeSelection.taskId && selected.nodeIds.includes(nodeSelection.nodeId) ? nodeSelection.nodeId : ''
  const summary = probeSummary(probes)
  const visibleTasks = probes.filter((task) => matchesProbe(task, nodes, query))
  const needle = query.trim().toLocaleLowerCase()
  const visibleNodes = nodes.flatMap((node) => {
    const owned = probes.filter((task) => task.nodeIds.includes(node.id))
    const nodeMatches = `${node.name} ${node.group}`.toLocaleLowerCase().includes(needle)
    const tasks = nodeMatches ? owned : owned.filter((task) => matchesProbe(task, [], query))
    return nodeMatches || tasks.length ? [{ node, tasks }] : []
  })
  const select = (task: ProbeTask, nodeId = '') => { setSelectedID(task.id); setNodeSelection({ taskId: task.id, nodeId }) }
  const reload = () => { refresh(); setHistoryRevision((value) => value + 1) }
  const actions = (task: ProbeTask) => <div className="probe-row-actions"><button type="button" aria-label={t('editProbeNamed', { name: task.name })} onClick={() => { setNotice(null); setEditor({ task }) }}>{t('editProbe')}</button><button type="button" className="danger" aria-label={t('deleteNamed', { name: task.name })} onClick={() => { setNotice(null); setDeleting(task) }}>{t('delete')}</button></div>
  return <div className="stack probes-panel">
    <section className="panel probe-controls" aria-label={t('probeTasks')}>
      <div className="section-heading"><div><h2>{t('probeTasks')}</h2><p className="muted">{t('probePageDescription')}</p></div><div className="button-row"><button type="button" onClick={reload}>{t('refreshProbes')}</button><button type="button" className="primary" onClick={() => { setNotice(null); setEditor({ task: null }) }}>{t('addTcpProbe')}</button></div></div>
      <dl className="probe-summary"><div><dt>{t('probeTotalTasks')}</dt><dd>{summary.tasks}</dd></div><div><dt>{t('probeAssignedNodes')}</dt><dd>{summary.nodes}</dd></div><div><dt>{t('probeLatestFailures')}</dt><dd>{summary.failures}</dd></div><div><dt>{t('probeWaitingResults')}</dt><dd>{summary.waiting}</dd></div></dl>
      <div className="probe-toolbar"><div className="view-toggle" role="group" aria-label={t('probeView')}><button type="button" aria-pressed={view === 'task'} onClick={() => setView('task')}>{t('probeTaskView')}</button><button type="button" aria-pressed={view === 'node'} onClick={() => setView('node')}>{t('probeNodeView')}</button></div><label className="probe-search">{t('searchProbes')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('probeSearchPlaceholder')} /></label></div>
    </section>
    {notice && <p className="notice success" role="status">{t(notice)}</p>}
    <section className="panel probe-list" aria-label={t(view === 'task' ? 'probeTaskView' : 'probeNodeView')}>
      {view === 'task' ? visibleTasks.length === 0 ? <p className="empty">{t(probes.length ? 'noMatchingProbes' : 'noProbes')}</p> : <div className="probe-rows">{visibleTasks.map((task) => <article className={`probe-task-row${selected?.id === task.id ? ' selected' : ''}`} key={task.id}>
        <div className="probe-row-heading"><button type="button" className="probe-task-main" aria-pressed={selected?.id === task.id} onClick={() => select(task)}><strong>{task.name}</strong><span>{probeTarget(task)}</span></button><div className="probe-task-meta"><span>{t('everySeconds', { seconds: task.intervalSeconds })}</span><span>{task.nodeIds.length ? t('probeNodeCount', { count: task.nodeIds.length }) : t('probeUnassigned')}</span></div>{actions(task)}</div>
        {task.nodeIds.length > 0 && <details className="probe-results"><summary>{t('probeRecentResults')}</summary><ul>{task.nodeIds.map((nodeId) => <li key={nodeId}><button type="button" onClick={() => select(task, nodeId)}>{nodes.find((node) => node.id === nodeId)?.name ?? nodeId}</button><ProbeResult task={task} nodeId={nodeId} /></li>)}</ul></details>}
      </article>)}</div> : visibleNodes.length === 0 ? <p className="empty">{t(nodes.length ? 'noMatchingNodes' : 'noNodes')}</p> : <div className="probe-rows">{visibleNodes.map(({ node, tasks }) => <article className="probe-node-row" key={node.id}>
        <div className="section-heading"><div><h3>{node.name}</h3>{node.group && <p className="muted">{node.group}</p>}</div><StatusBadge online={node.online} stale={node.connected && !node.online} /></div>
        {tasks.length === 0 ? <p className="muted">{t('probeNoAssignedTasks')}</p> : <div className="probe-node-tasks">{tasks.map((task) => <div key={task.id} className="probe-node-task"><button type="button" className="probe-task-main" onClick={() => select(task, node.id)}><strong>{task.name}</strong><span>{probeTarget(task)} · {t('everySeconds', { seconds: task.intervalSeconds })}</span></button><ProbeResult task={task} nodeId={node.id} />{actions(task)}</div>)}</div>}
      </article>)}</div>}
      <p className="muted probe-snapshot-note">{t('probeSnapshotHint')}</p>
    </section>
    {selected && <section className="panel probe-history" aria-label={t('historyTrends')}>
      <h2>{t('historyTitle', { name: selected.name })}</h2>
      <div className="probe-history-controls"><label>{t('probeHistoryTask')}<select value={selected.id} onChange={(event) => { setSelectedID(event.target.value); setNodeSelection({ taskId: event.target.value, nodeId: '' }) }}>{probes.map((task) => <option key={task.id} value={task.id}>{task.name}</option>)}</select></label>
        <label>{t('probeHistoryNode')}<select value={selectedNode} disabled={selected.nodeIds.length === 0} onChange={(event) => setNodeSelection({ taskId: selected.id, nodeId: event.target.value })}><option value="">{t('probeChooseNode')}</option>{selected.nodeIds.map((nodeId) => <option value={nodeId} key={nodeId}>{nodes.find((node) => node.id === nodeId)?.name ?? nodeId}</option>)}</select></label>
        <label>{t('probeHistoryRange')}<select value={range} onChange={(event) => { const value = event.target.value; if (value in probeHistoryRanges) setRange(value as ProbeHistoryRange) }}>{(Object.keys(probeHistoryRanges) as ProbeHistoryRange[]).map((value) => <option key={value} value={value}>{t(rangeLabels[value])}</option>)}</select></label>
      </div>
      {!selected.nodeIds.length ? <p className="empty">{t('assignProbe')}</p> : !selectedNode ? <p className="empty">{t('probeChooseNodeHint')}</p> : <ProbeHistory key={`${selected.id}:${selectedNode}:${range}:${historyRevision}`} taskId={selected.id} nodeId={selectedNode} range={range} onUnauthorized={onUnauthorized} />}
    </section>}
    {editor && <ProbeTaskDialog task={editor.task} nodes={nodes} onClose={() => setEditor(null)} onUnauthorized={onUnauthorized} onSaved={(edited) => { setEditor(null); setNotice(edited ? 'probeUpdated' : 'probeCreated'); reload() }} />}
    {deleting && <DeleteProbeDialog task={deleting} onClose={() => setDeleting(null)} onUnauthorized={onUnauthorized} onDeleted={() => { setDeleting(null); setNotice('probeDeleted'); reload() }} />}
  </div>
}

function ProbeResult({ task, nodeId }: { task: ProbeTask; nodeId: string }) {
  const { language, t } = useI18n()
  const result = latestProbeResult(task, nodeId)
  return <div className="probe-result">{!result ? <span className="status stale">{t('probeWaiting')}</span> : <><span className={`status ${result.success ? 'online' : 'offline'}`}>{result.success ? t('probeSuccess') : t('probeFailure')}{result.success && ` · ${result.latencyMillis.toFixed(1)} ms`}</span>{!result.success && result.error && <small className="muted">{result.error}</small>}<time className="muted" dateTime={result.receivedAt}>{formatTime(result.receivedAt, language)}</time></>}</div>
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
