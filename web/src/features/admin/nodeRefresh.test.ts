import { describe, expect, it } from 'vitest'

import { type LiveEvent, type Node } from '../../api/contracts'
import { createNodeRefreshBuffer } from './nodeRefresh'

const snapshot: Node = {
  id: 'node-1', name: 'Renamed edge', group: 'SG', hidden: false,
  hostname: '', os: '', kernel: '', architecture: '', virtualization: '',
  connected: true, online: true, lastReportAt: '2026-10-13T12:00:00Z', report: null,
}
const report: LiveEvent = {
  kind: 'report', nodeId: snapshot.id, connected: true, online: true,
  receivedAt: '2026-10-13T12:00:03Z', report: null,
}

describe('dashboard snapshot and in-flight live updates', () => {
  it('retains a later report and disconnect while applying fresh node metadata', () => {
    const pending = createNodeRefreshBuffer()
    pending.record(report)
    pending.record({ ...report, kind: 'disconnected', receivedAt: '2026-10-13T12:00:04Z', connected: false, online: false })
    const [node] = pending.replay([snapshot])
    expect(node).toMatchObject({ name: 'Renamed edge', group: 'SG', connected: false, online: false, lastReportAt: report.receivedAt })
  })

  it('does not resurrect a node deleted after its snapshot was read', () => {
    const pending = createNodeRefreshBuffer()
    pending.record({ ...report, kind: 'deleted', connected: false, online: false })
    expect(pending.replay([snapshot])).toEqual([])
  })

  it('ignores buffered reports older than the fetched snapshot', () => {
    const pending = createNodeRefreshBuffer()
    pending.record({ ...report, receivedAt: '2026-10-13T11:59:59Z' })
    expect(pending.replay([snapshot])).toEqual([snapshot])
  })

  it('isolates each refresh and reports overflow so the caller can restart rather than drop events', () => {
    const pending = createNodeRefreshBuffer()
    for (let count = 0; count < 512; count++) expect(pending.record(report)).toBe(true)
    expect(pending.record(report)).toBe(false)
    expect(createNodeRefreshBuffer().replay([snapshot])).toEqual([snapshot])
  })
})
