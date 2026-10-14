import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import type { Node, PublicPageConfig } from '../../api/contracts'
import { I18nProvider } from '../../i18n'
import { AlertsPanel } from './AlertsPanel'
import { PublicConfigPanel } from './PublicConfigPanel'
import { acknowledgePublicDraft, selectNodeIds, type PublicPageDraft } from './publicSelection'

const node: Node = {
  id: 'node-id', name: 'Public alias', group: 'Europe', hidden: false,
  hostname: 'PRIVATE_HOST', os: 'PRIVATE_OS', kernel: 'PRIVATE_KERNEL', architecture: 'PRIVATE_ARCH', virtualization: 'PRIVATE_VIRT',
  connected: true, online: true, lastReportAt: '2026-10-14T08:00:00Z', report: null,
}
const config: PublicPageConfig = { enabled: true, title: 'Service status', description: 'Availability', nodeIds: ['node-id'] }
const noop = () => {}

describe('Publication selection', () => {
  it('selects filtered groups without losing unseen selected nodes and never changes the original set', () => {
    const current = new Set(['hidden-by-search', 'group-a'])
    const next = selectNodeIds(current, ['group-a', 'group-b'], true)
    expect([...next]).toEqual(['hidden-by-search', 'group-a', 'group-b'])
    expect([...current]).toEqual(['hidden-by-search', 'group-a'])
    expect([...selectNodeIds(next, ['group-a', 'group-b'], false)]).toEqual(['hidden-by-search'])
  })

  it('deduplicates overlapping selections and tolerates already deselected IDs', () => {
    expect([...selectNodeIds(new Set(['a']), ['a', 'a', 'b'], true)]).toEqual(['a', 'b'])
    expect([...selectNodeIds(new Set(['a']), ['absent'], false)]).toEqual(['a'])
  })

  it('keeps saved values visible while old snapshots arrive, then releases them after acknowledgement', () => {
    const saved = { ...config, title: 'New page title', nodeIds: ['a', 'b'] }
    const draft: PublicPageDraft = { overrides: saved, saved }
    expect(acknowledgePublicDraft(draft, config)).toBe(draft)
    expect(acknowledgePublicDraft(draft, { ...saved, description: 'Older text' })).toBe(draft)
    expect(acknowledgePublicDraft(draft, { ...saved, nodeIds: ['a'] })).toBe(draft)
    const acknowledged = acknowledgePublicDraft(draft, { ...saved, nodeIds: ['b', 'a'] })
    expect(acknowledged).toEqual({ overrides: {}, saved: null })
    // Future server changes become visible instead of reactivating an old saved draft.
    expect(acknowledgePublicDraft(acknowledged, config)).toBe(acknowledged)
  })

  it('preserves dirty values across refreshed configuration and failed saves', () => {
    const draft: PublicPageDraft = { overrides: { title: 'Local draft', nodeIds: ['hidden-by-search'] }, saved: null }
    const refreshed = { ...config, title: 'External title', description: 'Updated description' }
    expect(acknowledgePublicDraft(draft, refreshed)).toBe(draft)
    const visible = { ...refreshed, ...draft.overrides }
    expect(visible.title).toBe('Local draft')
    expect(visible.description).toBe('Updated description')
    expect(visible.nodeIds).toEqual(['hidden-by-search'])
  })
})

describe('Remaining page boundaries', () => {
  it('limits publication drafts to page text and node display identities, even when admin models contain private fields', () => {
    const markup = renderToStaticMarkup(<I18nProvider><PublicConfigPanel nodes={[node]} config={config} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(markup).toContain('Service status')
    expect(markup).toContain('Public alias')
    expect(markup).toContain('Europe')
    expect(markup).not.toMatch(/PRIVATE_|node-id|lastReportAt|cpuUsagePercent/)
    expect(markup).toContain('Draft preview')
    expect(markup).toContain('1 / 1 nodes selected')
  })

  it('starts alert management with discovery and an actionable empty list; secrets are confined to explicit dialogs', () => {
    const markup = renderToStaticMarkup(<I18nProvider><AlertsPanel nodes={[]} channels={[]} rules={[]} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(markup).toContain('No notification channels yet')
    expect(markup).toContain('Create channel')
    expect(markup).not.toMatch(/type="password"|<dialog|name="bot_token"|name="secret"/)
  })
})
