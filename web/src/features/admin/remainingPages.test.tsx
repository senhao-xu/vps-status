import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import type { Node, PublicPageConfig } from '../../api/contracts'
import { I18nProvider } from '../../i18n'
import { AlertsPanel } from './AlertsPanel'
import { PublicConfigPanel } from './PublicConfigPanel'
import { acknowledgeSiteDraft, type SiteSettingsDraft } from './siteSettingsDraft'
import { NodeDetail } from './NodeDetail'

const node: Node = {
  id: 'node-id', name: 'Public alias', group: 'Europe', hidden: false, homepageVisible: false,
  hostname: 'PRIVATE_HOST', os: 'PRIVATE_OS', kernel: 'PRIVATE_KERNEL', architecture: 'PRIVATE_ARCH', virtualization: 'PRIVATE_VIRT',
  connected: true, online: true, lastReportAt: '2026-10-14T08:00:00Z', report: null,
}
const config: PublicPageConfig = { enabled: true, title: 'Service status', description: 'Availability', nodeIds: ['node-id'] }
const noop = () => {}

describe('Site settings drafts', () => {
  it('keeps saved values visible until acknowledged, independently of concurrent node visibility changes', () => {
    const saved = { enabled: false, title: 'New site name', description: 'New description' }
    const draft: SiteSettingsDraft = { overrides: saved, saved }
    expect(acknowledgeSiteDraft(draft, config)).toBe(draft)
    expect(acknowledgeSiteDraft(draft, { ...config, ...saved, description: 'Older text' })).toBe(draft)
    const acknowledged = acknowledgeSiteDraft(draft, { ...config, ...saved, nodeIds: ['new-enrollment'] })
    expect(acknowledged).toEqual({ overrides: {}, saved: null })
    expect(acknowledgeSiteDraft(acknowledged, config)).toBe(acknowledged)
  })

  it('preserves dirty values across refreshed configuration and failed saves', () => {
    const draft: SiteSettingsDraft = { overrides: { title: 'Local draft' }, saved: null }
    const refreshed = { ...config, title: 'External title', description: 'Updated description', nodeIds: ['another-node'] }
    expect(acknowledgeSiteDraft(draft, refreshed)).toBe(draft)
    const visible = { ...refreshed, ...draft.overrides }
    expect(visible.title).toBe('Local draft')
    expect(visible.description).toBe('Updated description')
    expect(visible.nodeIds).toEqual(['another-node'])
    expect(draft.overrides).not.toHaveProperty('nodeIds')
  })
})

describe('Remaining page boundaries', () => {
  it('offers site identity and access settings without a publication picker or preview', () => {
    const markup = renderToStaticMarkup(<I18nProvider><PublicConfigPanel config={config} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(markup).toContain('Service status')
    expect(markup).toContain('Site name')
    expect(markup).toContain('Save site settings')
    expect(markup).toContain('Access requires login')
    expect(markup).toContain('Node settings')
    expect(markup).toContain('aria-busy="false"')
    expect(markup).not.toMatch(/PRIVATE_|node-id|lastReportAt|cpuUsagePercent|Draft preview|Published nodes|publication-picker/)
    expect(markup).not.toContain('checked=""')
    const restricted = renderToStaticMarkup(<I18nProvider><PublicConfigPanel config={{ ...config, enabled: false }} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(restricted).toMatch(/type="checkbox" checked=""/)
  })

  it('keeps homepage visibility separate from the administrator overview setting', () => {
    const markup = renderToStaticMarkup(<I18nProvider><NodeDetail node={{ ...node, homepageVisible: true }} telemetry={{ status: 'loading' }} mutate={async () => {}} setOneTime={noop} onUnauthorized={noop} onSaved={noop} /></I18nProvider>)
    expect(markup).toMatch(/type="checkbox" name="homepage_visible" checked=""/)
    expect(markup).toContain('Homepage display')
    expect(markup).toMatch(/type="checkbox" name="hidden"\/>/)
    expect(markup).toContain('Hide in administrator overview')
  })

  it('starts alert management with discovery and an actionable empty list; secrets are confined to explicit dialogs', () => {
    const markup = renderToStaticMarkup(<I18nProvider><AlertsPanel nodes={[]} channels={[]} rules={[]} refresh={noop} onUnauthorized={noop} /></I18nProvider>)
    expect(markup).toContain('No notification channels yet')
    expect(markup).toContain('Create channel')
    expect(markup).not.toMatch(/type="password"|<dialog|name="bot_token"|name="secret"/)
  })
})
