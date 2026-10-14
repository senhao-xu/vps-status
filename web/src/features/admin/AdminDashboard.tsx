import { useCallback, useEffect, useRef, useState } from 'react'

import { APIError, apiRequest, errorMessage } from '../../api/client'
import {
  decodeChannels, decodeNodes, decodeProbes,
  decodePublicPageConfig, decodeRules, decodeStorageStats,
  reduceNodeEvent, type AlertRule, type Node, type NotificationChannel, type ProbeTask,
  type PublicPageConfig, type SessionResponse, type StorageStats, type TrafficTotal,
} from '../../api/contracts'
import { PreferenceControls } from '../../components/PreferenceControls'
import { useI18n } from '../../i18n'
import { useNodeLiveStream } from '../live/useNodeLiveStream'
import { AppearancePanel } from './AppearancePanel'
import { AlertsPanel } from './AlertsPanel'
import { StoragePanel } from './StoragePanel'
import { PublicConfigPanel } from './PublicConfigPanel'
import { SecurityPanel } from './SecurityPanel'
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
            {section === 'alerts' && <AlertsPanel nodes={data.nodes} channels={data.channels} rules={data.rules} refresh={refreshNodes} onUnauthorized={onUnauthorized} />}
            {section === 'storage' && <StoragePanel storage={data.storage} refresh={refreshNodes} onUnauthorized={onUnauthorized} />}
            {section === 'public' && <PublicConfigPanel nodes={data.nodes} config={data.publicPage} refresh={refreshNodes} onUnauthorized={onUnauthorized} />}
            {section === 'security' && <SecurityPanel session={session} refreshSession={refreshSession} onUnauthorized={onUnauthorized} />}
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

export function adminPrivateFieldSummary(node: Node): string {
  return `${node.hostname} ${node.os} ${node.kernel}`
}

export function currentMonthlyTraffic(rows: TrafficTotal[]): TrafficTotal | undefined {
  const month = new Date().toISOString().slice(0, 7)
  return rows.find((row) => row.period === month)
}
