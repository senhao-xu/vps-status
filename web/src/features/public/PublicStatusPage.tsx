import { useEffect, useState } from 'react'

import { apiRequest, errorMessage } from '../../api/client'
import { decodePublicStatus, type PublicStatus } from '../../api/contracts'
import { PreferenceControls } from '../../components/PreferenceControls'
import { FleetSummary, filterNodes, NodeFilters, type NodeView } from '../../components/NodeFilters'
import { NodeCardBody } from '../../components/NodeMetrics'
import { formatBytes } from '../../format'
import { useI18n } from '../../i18n'

type PublicState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; data: PublicStatus }

function PublicToolbar() {
  const { t } = useI18n()
  return <PreferenceControls><a href="/admin">{t('management')}</a></PreferenceControls>
}

export function PublicStatusPage() {
  const { t } = useI18n()
  const [state, setState] = useState<PublicState>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    let timer: number | null = null
    let disposed = false
    let sequence = 0
    const load = async () => {
      const requestSequence = ++sequence
      try {
        const data = await apiRequest('/api/v1/public/status', decodePublicStatus, { signal: controller.signal })
        if (!disposed && requestSequence === sequence) setState({ status: 'success', data })
      } catch (error) {
        if (!disposed && requestSequence === sequence) setState({ status: 'error', message: errorMessage(error, t) })
      } finally {
        if (!disposed) timer = window.setTimeout(() => void load(), 15_000)
      }
    }
    void load()
    return () => {
      disposed = true
      controller.abort()
      if (timer !== null) window.clearTimeout(timer)
    }
  }, [t])

  if (state.status === 'loading') return <main className="public-shell"><PublicToolbar /><p role="status">{t('loadingPublicStatus')}</p></main>
  if (state.status === 'error') return <main className="public-shell"><PublicToolbar /><div className="notice error" role="alert">{state.message}</div></main>
  return <PublicStatusView status={state.data} />
}

export function PublicStatusView({ status }: { status: PublicStatus }) {
  const { language, t } = useI18n()
  const [group, setGroup] = useState('')
  const [search, setSearch] = useState('')
  const [view, setView] = useState<NodeView>('cards')
  const groups = [...new Set(status.nodes.map((node) => node.group).filter(Boolean))].sort()
  const visible = filterNodes(status.nodes, group, search)
  if (!status.page.enabled) {
    return <main className="public-shell"><PublicToolbar /><section className="panel"><h1>{status.page.title}</h1><p>{t('publicDisabled')}</p></section></main>
  }
  return (
    <main className="public-shell">
      <PublicToolbar />
      <header className="public-header">
        <p className="eyebrow">{t('publicStatus')}</p>
        <h1>{status.page.title}</h1>
        {status.page.description && <p>{status.page.description}</p>}
        <p className="timezone-note">{t('timezoneNote')}</p>
      </header>
      <div className="public-overview"><FleetSummary total={status.nodes.length} online={status.nodes.filter((node) => node.online).length} groups={groups.length} />
        <NodeFilters groups={groups} group={group} onGroup={setGroup} search={search} onSearch={setSearch} view={view} onView={setView} /></div>
      <section className={`public-grid ${view === 'list' ? 'node-list' : ''}`} aria-label={t('publishedNodes')}>
        {visible.length === 0 ? <p className="empty">{status.nodes.length === 0 ? t('noPublishedNodes') : t('noMatchingNodes')}</p> : visible.map((node, index) => (
          <article className="public-node" key={`${node.group}:${node.name}:${index}`}>
            <NodeCardBody name={node.name} group={node.group} online={node.online} values={node} extra={<dl className="monthly-traffic">
              <div><dt>{t('monthReceived')}</dt><dd>{formatBytes(node.monthReceiveBytes, language)}</dd></div>
              <div><dt>{t('monthSent')}</dt><dd>{formatBytes(node.monthTransmitBytes, language)}</dd></div>
            </dl>} />
          </article>
        ))}
      </section>
      <footer>{t('readOnlyPublicView')}</footer>
    </main>
  )
}
