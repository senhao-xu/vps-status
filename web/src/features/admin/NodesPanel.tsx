import { useCallback, useEffect, useState } from 'react'

import { APIError, apiRequest, errorMessage } from '../../api/client'
import { decodeHostPoints, decodeTraffic, type HostPoint, type Node, type TrafficTotal } from '../../api/contracts'
import { FleetSummary, filterNodes, NodeFilters, type NodeView } from '../../components/NodeFilters'
import { NodeCardBody } from '../../components/NodeMetrics'
import { useI18n } from '../../i18n'
import { EnrollmentDialog } from './EnrollmentDialog'
import { NodeDetail } from './NodeDetail'

export type Mutate = (action: () => Promise<void>, success: string) => Promise<void>
export type NodeTelemetryState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; history: HostPoint[]; traffic: TrafficTotal[] }

export function NodesPanel({ nodes, mutate, refreshNodes, onUnauthorized }: {
  nodes: Node[]; mutate: Mutate; refreshNodes: () => void; onUnauthorized: () => void
}) {
  const { t } = useI18n()
  const [selectedID, setSelectedID] = useState('')
  const [telemetry, setTelemetry] = useState<NodeTelemetryState>({ status: 'loading' })
  const [oneTime, setOneTime] = useState('')
  const [search, setSearch] = useState('')
  const [group, setGroup] = useState('')
  const [view, setView] = useState<NodeView>('cards')
  const [installing, setInstalling] = useState(false)
  const [showHidden, setShowHidden] = useState(false)
  const selected = nodes.find((node) => node.id === selectedID)
  const overview = nodes.filter((node) => showHidden || !node.hidden)
  const groups = [...new Set(overview.map((node) => node.group).filter(Boolean))].sort()
  const visible = filterNodes(overview, group, search)
  const onOnline = useCallback((nodeId: string) => { setSelectedID(nodeId); refreshNodes() }, [refreshNodes])

  useEffect(() => {
    if (!selected) return
    setTelemetry({ status: 'loading' })
    const controller = new AbortController()
    const to = new Date(), from = new Date(to.getTime() - 24 * 60 * 60 * 1000)
    const query = new URLSearchParams({ node_id: selected.id, resolution: '1m', from: from.toISOString(), to: to.toISOString() })
    void Promise.all([
      apiRequest(`/api/v1/admin/telemetry/host?${query}`, decodeHostPoints, { signal: controller.signal }),
      apiRequest(`/api/v1/admin/telemetry/traffic?node_id=${encodeURIComponent(selected.id)}`, decodeTraffic, { signal: controller.signal }),
    ]).then(([history, traffic]) => { if (!controller.signal.aborted) setTelemetry({ status: 'success', history, traffic }) })
      .catch((caught: unknown) => {
        if (controller.signal.aborted) return
        if (caught instanceof APIError && caught.status === 401) { onUnauthorized(); return }
        setTelemetry({ status: 'error', message: errorMessage(caught, t) })
      })
    return () => controller.abort()
  }, [selected?.id, onUnauthorized, t])

  return <div className="stack">
    <div className="section-heading fleet-heading"><div><h2>{t('fleetOverview')}</h2><p className="muted">{t('onlineCount', { online: nodes.filter((node) => node.online).length, total: nodes.length })}</p></div><button className="primary" onClick={() => setInstalling(true)}>+ {t('addNode')}</button></div>
    <FleetSummary total={nodes.length} online={nodes.filter((node) => node.online).length} groups={groups.length} />
    <NodeFilters groups={groups} group={group} onGroup={setGroup} search={search} onSearch={setSearch} view={view} onView={setView} />
    {nodes.some((node) => node.hidden) && <label className="check"><input type="checkbox" checked={showHidden} onChange={(event) => setShowHidden(event.target.checked)} />{t('showHiddenNodes')}</label>}
    <div className={`node-grid ${view === 'list' ? 'node-list' : ''}`}>
      {visible.length === 0 ? <div className="empty panel"><p>{nodes.length === 0 ? t('noNodes') : t('noMatchingNodes')}</p>{nodes.length > 0 && <button onClick={() => { setGroup(''); setSearch('') }}>{t('clearFilters')}</button>}</div> : visible.map((node) => <button key={node.id} className={`node-card ${selected?.id === node.id ? 'selected' : ''}`} aria-label={t('viewNode') + ': ' + node.name} aria-pressed={selected?.id === node.id} onClick={() => { setSelectedID(node.id); setOneTime('') }}>
        <NodeCardBody name={node.name} group={node.group} online={node.online} stale={node.connected && !node.online} values={node.report} />
      </button>)}
    </div>
    {selected && <><button className="back-button" onClick={() => { setSelectedID(''); setOneTime('') }}>← {t('backToNodes')}</button><NodeDetail key={selected.id} node={selected} telemetry={telemetry} mutate={mutate} setOneTime={setOneTime} onUnauthorized={onUnauthorized} />{oneTime && <pre className="secret-once" role="status">{oneTime}</pre>}</>}
    {installing && <EnrollmentDialog onClose={() => setInstalling(false)} onOnline={onOnline} onViewNode={setSelectedID} onUnauthorized={onUnauthorized} />}
  </div>
}
