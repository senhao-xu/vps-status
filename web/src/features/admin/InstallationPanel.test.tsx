import { renderToStaticMarkup } from 'react-dom/server'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { I18nProvider, translate } from '../../i18n'
import { EnrollmentDialog } from './EnrollmentDialog'
import { InstallationPanel } from './InstallationPanel'
import { NodesPanel } from './NodesPanel'

afterEach(() => vi.unstubAllGlobals())

describe('installation entry points', () => {
  it('shows an observable loading state before editing saved settings', () => {
    const markup = renderToStaticMarkup(<I18nProvider><InstallationPanel onUnauthorized={() => {}} /></I18nProvider>)
    expect(markup).toContain('Installation settings')
    expect(markup).toContain('role="status">Loading installation settings…')
    expect(markup).not.toContain('<form')
  })

  it('asks only for a name and disables enrollment until settings have loaded', () => {
    const markup = renderToStaticMarkup(<I18nProvider><EnrollmentDialog onClose={() => {}} onOnline={() => {}} onViewNode={() => {}} onUnauthorized={() => {}} onOpenSettings={() => {}} /></I18nProvider>)
    expect(markup).toContain('name="node_name"')
    expect(markup).not.toContain('name="control_url"')
    expect(markup).toContain('disabled="">Create one-time install command')
    expect(markup).toContain('aria-labelledby="enrollment-title"')
  })

  it('provides a first-node action in the empty overview', () => {
    const markup = renderToStaticMarkup(<I18nProvider><NodesPanel nodes={[]} mutate={async () => {}} refreshNodes={() => {}} onUnauthorized={() => {}} onOpenSettings={() => {}} /></I18nProvider>)
    expect(markup).toContain('Add your first Linux server')
    expect(markup.match(/Add node/g)).toHaveLength(2)
    expect(markup).toContain('Showing 0 of 0 nodes')
  })

  it('provides Chinese settings navigation and actionable development-build guidance', () => {
    vi.stubGlobal('window', { navigator: { language: 'zh-CN' } })
    const markup = renderToStaticMarkup(<I18nProvider><InstallationPanel onUnauthorized={() => {}} /></I18nProvider>)
    expect(markup).toContain('安装设置')
    expect(translate('zh', 'agentReleaseRequired')).toContain('已发布的 Agent 版本')
    expect(translate('en', 'agentReleaseRequired')).toContain('published Agent version')
    expect(translate('zh', 'connectionAddressRequired')).toContain('HTTPS')
    expect(translate('en', 'releaseVersionHelp')).toContain('bootstrap-agent.sh')
  })
})
