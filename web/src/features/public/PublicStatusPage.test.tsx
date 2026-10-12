import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import type { PublicStatus } from '../../api/contracts'
import { PublicStatusView } from './PublicStatusPage'

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
    const markup = renderToStaticMarkup(<PublicStatusView status={status} />)
    expect(markup).toContain('Edge alias')
    expect(markup).toContain('Offline')
    expect(markup).toContain('Unavailable')
    expect(markup).not.toContain('real-hostname-secret')
    expect(markup).not.toContain('private-kernel')
  })

  it('keeps a disabled page free of node content', () => {
    const markup = renderToStaticMarkup(<PublicStatusView status={{
      page: { enabled: false, title: 'Private fleet', description: '' },
      nodes: [],
    }} />)
    expect(markup).toContain('not enabled')
    expect(markup).not.toContain('Published nodes')
  })
})
