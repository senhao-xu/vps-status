import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ProbeTask } from '../../api/contracts'
import { I18nProvider } from '../../i18n'
import { ProbeTaskDialog } from './ProbeTaskDialog'
import { ProbesPanel } from './ProbesPanel'

const task: ProbeTask = { id: 'probe', name: 'Gateway', host: 'example.com', port: 443, intervalSeconds: 30, updatedAt: '2026-10-14T07:00:00Z', nodeIds: ['node-a', 'node-b'], latest: [] }
const renderPanel = (probes: ProbeTask[]) => renderToStaticMarkup(<I18nProvider><ProbesPanel probes={probes} nodes={[]} refresh={() => {}} onUnauthorized={() => {}} /></I18nProvider>)

afterEach(() => vi.unstubAllGlobals())

describe('TCP panel semantics', () => {
  it('requires explicit history node selection and shows waiting samples without zero-success charts', () => {
    const markup = renderPanel([task])
    expect(markup).toContain('Execution node<select')
    expect(markup).toContain('<option value="" selected="">Choose an execution node</option>')
    expect(markup).toContain('Choose an execution node above')
    expect(markup.match(/class="status stale">Waiting for result/g)).toHaveLength(2)
    expect(markup).not.toContain('<svg')
    expect(markup).toContain('Last 7 days</option>')
    expect(markup).toContain('Last 30 days</option>')
    expect(markup).not.toContain('Last 7 days · 10 minutes')
    expect(markup).not.toContain('Success · 0.0 ms')
    expect(markup).not.toContain('<form')
  })

  it('keeps add/view/search actions available for empty data', () => {
    const markup = renderPanel([])
    expect(markup).toContain('Add TCP probe')
    expect(markup).toContain('By task')
    expect(markup).toContain('By node')
    expect(markup).toContain('Search probes')
    expect(markup).toContain('No TCP probes configured.')
  })

  it('explains unassigned tasks and disables the execution-node selector', () => {
    const markup = renderPanel([{ ...task, nodeIds: [] }])
    expect(markup).toContain('Unassigned')
    expect(markup).toContain('Execution node<select disabled=""')
    expect(markup).toContain('Assign this probe to a node to collect history.')
  })

  it('renders localized add/edit flows with existing bounds and existing assignments', () => {
    vi.stubGlobal('window', { navigator: { language: 'zh-CN' } })
    const markup = renderToStaticMarkup(<I18nProvider><ProbeTaskDialog task={task} nodes={[]} onClose={() => {}} onSaved={() => {}} onUnauthorized={() => {}} /></I18nProvider>)
    expect(markup).toContain('编辑 TCP 探测')
    expect(markup).toContain('保存探测')
    expect(markup).toContain('aria-busy="false"')
    expect(markup).toMatch(/<input(?=[^>]*name="interval")(?=[^>]*min="5")(?=[^>]*max="3600")/)
    expect(markup).toMatch(/<input(?=[^>]*name="port")(?=[^>]*min="1")(?=[^>]*max="65535")/)
    expect(markup).toContain('value="Gateway"')
    expect(markup).toContain('清空选择')
    expect(markup).toContain('全选当前显示')
  })
})
