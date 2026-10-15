import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ProbeTask } from '../../api/contracts'
import { I18nProvider } from '../../i18n'
import { ProbeDetailDialog } from './ProbeDetailDialog'
import { ProbeTaskDialog } from './ProbeTaskDialog'
import { ProbesPanel } from './ProbesPanel'

const task: ProbeTask = { id: 'probe', name: 'Gateway', host: 'example.com', port: 443, intervalSeconds: 30, autoJoin: false, updatedAt: '2026-10-14T07:00:00Z', nodeIds: ['node-a', 'node-b'], latest: [] }
const noop = () => {}
const renderPanel = (probes: ProbeTask[]) => renderToStaticMarkup(<I18nProvider><ProbesPanel probes={probes} nodes={[]} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
const renderDetails = (probe: ProbeTask, nodeId = '') => renderToStaticMarkup(<I18nProvider><ProbeDetailDialog task={probe} tasks={[probe]} nodes={[]} nodeId={nodeId} onSelect={noop} onClose={noop} onRefresh={noop} revision={0} onUnauthorized={noop} /></I18nProvider>)

afterEach(() => vi.unstubAllGlobals())

describe('Latency check panel semantics', () => {
  it('opens history through explicit detail actions and keeps the list free of an automatic history selection', () => {
    const markup = renderPanel([task])
    expect(markup).toContain('View details')
    expect(markup).toContain('aria-label="View details for Gateway"')
    expect(markup).toContain('aria-haspopup="dialog"')
    expect(markup.match(/class="status stale">Waiting for result/g)).toHaveLength(2)
    expect(markup).not.toContain('<dialog')
    expect(markup).not.toContain('<select')
    expect(markup).not.toContain('<svg')
    expect(markup).not.toContain('Success · 0.0 ms')
    expect(markup).not.toContain('<form')
  })

  it('keeps add/view/search actions available for empty data with the renamed wording', () => {
    const markup = renderPanel([])
    expect(markup).toContain('Add latency check')
    expect(markup).toContain('By task')
    expect(markup).toContain('By node')
    expect(markup).toContain('Search latency checks')
    expect(markup).toContain('No latency checks configured.')
    expect(markup).not.toContain('TCP probe')
  })

  it('shows metadata and all recent node results while requiring an explicit node before history loads', () => {
    const markup = renderDetails(task)
    expect(markup).toContain('Gateway · Latency check details')
    expect(markup).toContain('<dt>Target</dt><dd>example.com:443</dd>')
    expect(markup).toContain('<dt>Method</dt><dd>TCP connection latency</dd>')
    expect(markup).toContain('every 30s')
    expect(markup).toContain('<option value="" selected="">Choose an execution node</option>')
    expect(markup).toContain('Choose an execution node above')
    expect(markup).toContain('Last 7 days</option>')
    expect(markup).toContain('Last 30 days</option>')
    expect(markup).toContain('earlier configurations')
    expect(markup.match(/class="status stale">Waiting for result/g)).toHaveLength(2)
    expect(markup).not.toContain('Loading latency history')
    expect(markup).not.toContain('<svg')
  })

  it('shows the loading state for a selected node and never substitutes another node', () => {
    const markup = renderDetails(task, 'node-b')
    expect(markup).toContain('<option value="node-b" selected="">node-b</option>')
    expect(markup).toContain('role="status">Loading latency history')
    expect(markup).not.toContain('Choose an execution node above')
    expect(markup).not.toContain('<svg')
  })

  it('explains unassigned tasks and disables the execution-node selector in details', () => {
    const markup = renderDetails({ ...task, nodeIds: [] })
    expect(markup).toContain('Unassigned')
    expect(markup).toContain('Execution node<select disabled=""')
    expect(markup).toContain('Assign this latency check to a node to collect history.')
    expect(markup).not.toContain('Loading latency history')
  })

  it('ignores removed assignment and old configuration outcomes when rendering current details', () => {
    const markup = renderDetails({ ...task, nodeIds: ['node-b'], latest: [
      { nodeId: 'node-a', success: true, latencyMillis: 999, error: '', receivedAt: '2026-10-14T08:00:00Z' },
      { nodeId: 'node-b', success: true, latencyMillis: 123, error: '', receivedAt: task.updatedAt },
    ] })
    expect(markup).not.toContain('node-a')
    expect(markup).not.toContain('999.0 ms')
    expect(markup).not.toContain('123.0 ms')
    expect(markup).toContain('Waiting for result')
  })

  it('renders localized details and add/edit flows with existing TCP bounds', () => {
    vi.stubGlobal('window', { navigator: { language: 'zh-CN' } })
    const markup = renderToStaticMarkup(<I18nProvider><ProbeTaskDialog task={task} nodes={[]} onClose={noop} onSaved={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(markup).toContain('编辑延迟检测')
    expect(markup).toContain('保存检测')
    expect(markup).toContain('TCP 建连延迟')
    expect(markup).toContain('aria-busy="false"')
    expect(markup).toMatch(/<input(?=[^>]*name="interval")(?=[^>]*min="5")(?=[^>]*max="3600")/)
    expect(markup).toMatch(/<input(?=[^>]*name="target")(?=[^>]*value="example.com:443")/)
    expect(markup).toContain('value="Gateway"')
    expect(markup).toContain('新节点自动加入')
    expect(markup).toContain('暂无节点。')
    const details = renderDetails(task)
    expect(details).toContain('Gateway · 延迟检测详情')
    expect(details).toContain('TCP 建连延迟')
    expect(details).toContain('请选择执行节点')
    expect(details).not.toContain('TCP 探测')
  })
})
