import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import type { PublicStatus } from '../../api/contracts'
import { I18nProvider } from '../../i18n'
import { ThemeProvider } from '../../theme'
import { PublicStatusView } from './PublicStatusPage'

function renderView(status: PublicStatus): string {
  return renderToStaticMarkup(<ThemeProvider><I18nProvider><PublicStatusView status={status} /></I18nProvider></ThemeProvider>)
}

describe('PublicStatusView', () => {
  it('renders allowed health fields with text status and no private sentinels', () => {
    const status: PublicStatus = {
      page: { enabled: true, title: 'Fleet health', description: 'Public availability' },
      nodes: [{
        name: 'Edge alias', group: 'SG', online: false, uptimeSeconds: null, cpuUsagePercent: null,
        memoryUsedBytes: null, memoryTotalBytes: null, diskUsedBytes: null, diskTotalBytes: null,
        receiveBytesPerSecond: null, transmitBytesPerSecond: null, monthReceiveBytes: 1024, monthTransmitBytes: 2048,
      }],
    }
    const markup = renderView(status)
    expect(markup).toContain('Edge alias')
    expect(markup).toContain('Offline')
    expect(markup).toContain('Unavailable')
    expect(markup).not.toContain('real-hostname-secret')
    expect(markup).not.toContain('private-kernel')
  })

  it('keeps a disabled page free of node content', () => {
    const markup = renderView({
      page: { enabled: false, title: 'Private fleet', description: '' },
      nodes: [],
    })
    expect(markup).toContain('not enabled')
    expect(markup).not.toContain('Published nodes')
  })

  it('never presents retained offline sample values as current resource bars or network speed', () => {
    const markup = renderView({
      page: { enabled: true, title: 'Fleet', description: '' },
      nodes: [{ name: 'Offline edge', group: 'SG', online: false, uptimeSeconds: 123456, cpuUsagePercent: 67.8,
        memoryUsedBytes: 512, memoryTotalBytes: 1024, diskUsedBytes: 768, diskTotalBytes: 1024,
        receiveBytesPerSecond: 999999, transmitBytesPerSecond: 888888, monthReceiveBytes: 1024, monthTransmitBytes: 2048 }],
    })
    expect(markup).toContain('Current metrics unavailable while offline')
    expect(markup).toContain('width:0%')
    expect(markup).not.toContain('67.8%')
    expect(markup).not.toContain('50.0%')
    expect(markup).not.toContain('75.0%')
    expect(markup).toContain('1.00 KiB')
    expect(markup).toContain('2.00 KiB')
  })
})
