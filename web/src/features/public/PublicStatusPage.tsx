import { useEffect, useState } from 'react'

import { apiRequest, errorMessage } from '../../api/client'
import { decodePublicStatus, type PublicStatus } from '../../api/contracts'
import { formatBytes, formatDuration, formatPercent, formatRate, ratio } from '../../format'

type PublicState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; data: PublicStatus }

export function PublicStatusPage() {
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
        if (!disposed && requestSequence === sequence) setState({ status: 'error', message: errorMessage(error) })
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
  }, [])

  if (state.status === 'loading') return <main className="public-shell"><p role="status">Loading public status…</p></main>
  if (state.status === 'error') return <main className="public-shell"><div className="notice error" role="alert">{state.message}</div></main>
  return <PublicStatusView status={state.data} />
}

export function PublicStatusView({ status }: { status: PublicStatus }) {
  if (!status.page.enabled) {
    return <main className="public-shell"><section className="panel"><h1>{status.page.title}</h1><p>This public status page is not enabled.</p></section></main>
  }
  return (
    <main className="public-shell">
      <header className="public-header">
        <p className="eyebrow">Public status</p>
        <h1>{status.page.title}</h1>
        {status.page.description && <p>{status.page.description}</p>}
        <p className="timezone-note">Times use your browser timezone. Traffic totals use the current UTC month.</p>
      </header>
      <section className="public-grid" aria-label="Published nodes">
        {status.nodes.length === 0 ? <p className="empty">No nodes are published.</p> : status.nodes.map((node, index) => (
          <article className="public-node" key={`${node.group}:${node.name}:${index}`}>
            <div className="section-heading">
              <div><p className="muted">{node.group || 'Ungrouped'}</p><h2>{node.name}</h2></div>
              <span className={`status ${node.online ? 'online' : 'offline'}`}>{node.online ? 'Online' : 'Offline'}</span>
            </div>
            <dl className="metric-grid">
              <div><dt>Uptime</dt><dd>{formatDuration(node.uptimeSeconds)}</dd></div>
              <div><dt>CPU</dt><dd>{formatPercent(node.cpuUsagePercent)}</dd></div>
              <div><dt>Memory</dt><dd>{formatPercent(ratio(node.memoryUsedBytes, node.memoryTotalBytes))}</dd></div>
              <div><dt>Disk</dt><dd>{formatPercent(ratio(node.diskUsedBytes, node.diskTotalBytes))}</dd></div>
              <div><dt>Download</dt><dd>{formatRate(node.receiveBytesPerSecond)}</dd></div>
              <div><dt>Upload</dt><dd>{formatRate(node.transmitBytesPerSecond)}</dd></div>
              <div><dt>Month received</dt><dd>{formatBytes(node.monthReceiveBytes)}</dd></div>
              <div><dt>Month sent</dt><dd>{formatBytes(node.monthTransmitBytes)}</dd></div>
            </dl>
          </article>
        ))}
      </section>
      <footer>VPS Status · read-only public view</footer>
    </main>
  )
}
