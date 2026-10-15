import { useRef, useState } from 'react'

import { type Node, type ProbeTask } from '../../api/contracts'
import { StatusBadge } from '../../components/NodeMetrics'
import { useI18n, type TranslationKey } from '../../i18n'
import { ProbeDetailDialog } from './ProbeDetailDialog'
import { ProbeResult } from './ProbeResult'
import { DeleteProbeDialog, ProbeTaskDialog } from './ProbeTaskDialog'
import { matchesProbe, probeSummary, probeTarget } from './probePresentation'

export function ProbesPanel({ probes, nodes, refresh, onUnauthorized }: {
  probes: ProbeTask[]; nodes: Node[]; refresh: () => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [view, setView] = useState<'task' | 'node'>('task')
  const [query, setQuery] = useState('')
  const [detail, setDetail] = useState<{ taskId: string; nodeId: string } | null>(null)
  const addButton = useRef<HTMLButtonElement>(null)
  const [historyRevision, setHistoryRevision] = useState(0)
  const [editor, setEditor] = useState<{ task: ProbeTask | null } | null>(null)
  const [deleting, setDeleting] = useState<ProbeTask | null>(null)
  const [notice, setNotice] = useState<TranslationKey | null>(null)
  const selected = detail ? probes.find((task) => task.id === detail.taskId) : undefined
  // Invalidate removed identities before rendering history; re-adding an assignment
  // must not silently restore a selection the operator no longer has open.
  if (detail && (!selected || (detail.nodeId && !selected.nodeIds.includes(detail.nodeId)))) {
    setDetail(selected ? { taskId: selected.id, nodeId: '' } : null)
  }
  const selectedNode = selected && detail && selected.nodeIds.includes(detail.nodeId) ? detail.nodeId : ''
  const summary = probeSummary(probes)
  const visibleTasks = probes.filter((task) => matchesProbe(task, nodes, query))
  const needle = query.trim().toLocaleLowerCase()
  const visibleNodes = nodes.flatMap((node) => {
    const owned = probes.filter((task) => task.nodeIds.includes(node.id))
    const nodeMatches = `${node.name} ${node.group}`.toLocaleLowerCase().includes(needle)
    const tasks = nodeMatches ? owned : owned.filter((task) => matchesProbe(task, [], query))
    return nodeMatches || tasks.length ? [{ node, tasks }] : []
  })
  const select = (task: ProbeTask, nodeId = '') => setDetail({ taskId: task.id, nodeId })
  const reload = () => { refresh(); setHistoryRevision((value) => value + 1) }
  const actions = (task: ProbeTask, nodeId = '') => <div className="probe-row-actions"><button type="button" aria-haspopup="dialog" aria-label={t('viewProbeNamed', { name: task.name })} onClick={() => select(task, nodeId)}>{t('viewProbeDetails')}</button><button type="button" aria-label={t('editProbeNamed', { name: task.name })} onClick={() => { setNotice(null); setEditor({ task }) }}>{t('editProbe')}</button><button type="button" className="danger" aria-label={t('deleteNamed', { name: task.name })} onClick={() => { setNotice(null); setDeleting(task) }}>{t('delete')}</button></div>
  return <div className="stack probes-panel">
    <section className="panel probe-controls" aria-label={t('probeTasks')}>
      <div className="section-heading"><div><h2>{t('probeTasks')}</h2><p className="muted">{t('probePageDescription')}</p></div><div className="button-row"><button type="button" onClick={reload}>{t('refreshProbes')}</button><button ref={addButton} type="button" className="primary" onClick={() => { setNotice(null); setEditor({ task: null }) }}>{t('addTcpProbe')}</button></div></div>
      <dl className="probe-summary"><div><dt>{t('probeTotalTasks')}</dt><dd>{summary.tasks}</dd></div><div><dt>{t('probeAssignedNodes')}</dt><dd>{summary.nodes}</dd></div><div><dt>{t('probeLatestFailures')}</dt><dd>{summary.failures}</dd></div><div><dt>{t('probeWaitingResults')}</dt><dd>{summary.waiting}</dd></div></dl>
      <div className="probe-toolbar"><div className="view-toggle" role="group" aria-label={t('probeView')}><button type="button" aria-pressed={view === 'task'} onClick={() => setView('task')}>{t('probeTaskView')}</button><button type="button" aria-pressed={view === 'node'} onClick={() => setView('node')}>{t('probeNodeView')}</button></div><label className="probe-search">{t('searchProbes')}<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('probeSearchPlaceholder')} /></label></div>
    </section>
    {notice && <p className="notice success" role="status">{t(notice)}</p>}
    <section className="panel probe-list" aria-label={t(view === 'task' ? 'probeTaskView' : 'probeNodeView')}>
      {view === 'task' ? visibleTasks.length === 0 ? <p className="empty">{t(probes.length ? 'noMatchingProbes' : 'noProbes')}</p> : <div className="probe-rows">{visibleTasks.map((task) => <article className="probe-task-row" key={task.id}>
        <div className="probe-row-heading"><button type="button" className="probe-task-main" aria-haspopup="dialog" onClick={() => select(task)}><strong>{task.name}</strong><span>{probeTarget(task)}</span></button><div className="probe-task-meta"><span>{t('probeTcpMethod')}</span><span>{t('everySeconds', { seconds: task.intervalSeconds })}</span><span>{task.nodeIds.length ? t('probeNodeCount', { count: task.nodeIds.length }) : t('probeUnassigned')}</span></div>{actions(task)}</div>
        {task.nodeIds.length > 0 && <details className="probe-results"><summary>{t('probeRecentResults')}</summary><ul>{task.nodeIds.map((nodeId) => <li key={nodeId}><button type="button" className="probe-result-link" aria-haspopup="dialog" onClick={() => select(task, nodeId)}><span>{nodes.find((node) => node.id === nodeId)?.name ?? nodeId}</span><ProbeResult task={task} nodeId={nodeId} /></button></li>)}</ul></details>}
      </article>)}</div> : visibleNodes.length === 0 ? <p className="empty">{t(nodes.length ? 'noMatchingNodes' : 'noNodes')}</p> : <div className="probe-rows">{visibleNodes.map(({ node, tasks }) => <article className="probe-node-row" key={node.id}>
        <div className="section-heading"><div><h3>{node.name}</h3>{node.group && <p className="muted">{node.group}</p>}</div><StatusBadge online={node.online} stale={node.connected && !node.online} /></div>
        {tasks.length === 0 ? <p className="muted">{t('probeNoAssignedTasks')}</p> : <div className="probe-node-tasks">{tasks.map((task) => <div key={task.id} className="probe-node-task"><button type="button" className="probe-task-main" aria-haspopup="dialog" onClick={() => select(task, node.id)}><strong>{task.name}</strong><span>{probeTarget(task)} · {t('everySeconds', { seconds: task.intervalSeconds })}</span></button><button type="button" className="probe-result-button" aria-haspopup="dialog" onClick={() => select(task, node.id)}><ProbeResult task={task} nodeId={node.id} /></button>{actions(task, node.id)}</div>)}</div>}
      </article>)}</div>}
      <p className="muted probe-snapshot-note">{t('probeSnapshotHint')}</p>
    </section>
    {selected && detail && <ProbeDetailDialog task={selected} tasks={probes} nodes={nodes} nodeId={selectedNode} onSelect={(taskId, nodeId) => setDetail({ taskId, nodeId })} onClose={() => setDetail(null)} onRefresh={reload} revision={historyRevision} onUnauthorized={onUnauthorized} fallbackFocus={addButton} />}
    {editor && <ProbeTaskDialog task={editor.task ? probes.find((task) => task.id === editor.task?.id) ?? editor.task : null} nodes={nodes} onClose={() => setEditor(null)} onUnauthorized={onUnauthorized} onSaved={(edited) => { setEditor(null); setNotice(edited ? 'probeUpdated' : 'probeCreated'); reload() }} />}
    {deleting && <DeleteProbeDialog task={deleting} onClose={() => setDeleting(null)} onUnauthorized={onUnauthorized} onDeleted={() => { setDeleting(null); setNotice('probeDeleted'); reload() }} />}
  </div>
}
