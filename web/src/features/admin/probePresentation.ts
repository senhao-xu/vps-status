import type { Node, ProbeLatest, ProbeTask } from '../../api/contracts'

export const probeHistoryRanges = {
  '24h': 24,
  '7d': 7 * 24,
  '30d': 30 * 24,
} as const
export type ProbeHistoryRange = keyof typeof probeHistoryRanges

export function probeTarget(probe: Pick<ProbeTask, 'host' | 'port'>): string {
  return `${probe.host.includes(':') ? `[${probe.host}]` : probe.host}:${probe.port}`
}

// Ignore residual results after assignments change; a missing sample is waiting,
// never a zero-latency success. Select the newest result if a snapshot has duplicates.
export function latestProbeResult(probe: ProbeTask, nodeId: string): ProbeLatest | undefined {
  if (!probe.nodeIds.includes(nodeId)) return undefined
  // Configuration timestamps are stored as whole seconds. Samples in that
  // second may precede the edit, so wait until the next second to trust them.
  const freshAfter = (Math.floor(Date.parse(probe.updatedAt) / 1000) + 1) * 1000
  return probe.latest.filter((result) => result.nodeId === nodeId && Date.parse(result.receivedAt) >= freshAfter)
    .reduce<ProbeLatest | undefined>((latest, result) => !latest || Date.parse(result.receivedAt) > Date.parse(latest.receivedAt) ? result : latest, undefined)
}

export function probeSummary(probes: ProbeTask[]) {
  const assigned = new Set<string>()
  let failures = 0, waiting = 0
  for (const probe of probes) {
    for (const nodeId of probe.nodeIds) {
      assigned.add(nodeId)
      const result = latestProbeResult(probe, nodeId)
      if (!result) waiting++
      else if (!result.success) failures++
    }
  }
  return { tasks: probes.length, nodes: assigned.size, failures, waiting }
}

export function matchesProbe(probe: ProbeTask, nodes: Node[], query: string): boolean {
  const needle = query.trim().toLocaleLowerCase()
  const names = nodes.filter((node) => probe.nodeIds.includes(node.id)).flatMap((node) => [node.name, node.group])
  return [probe.name, probeTarget(probe), ...names].some((text) => text.toLocaleLowerCase().includes(needle))
}

// QueryProbe uses [from, to). Match the retention's coarser bucket boundaries
// so one coarse bucket never overlaps the next fine window.
export function probeHistoryQueries(taskId: string, nodeId: string, range: ProbeHistoryRange, now = new Date()): string[] {
  const hour = 60 * 60 * 1000
  const end = now.getTime()
  const recentBoundary = Math.ceil((end - 24 * hour) / (10 * 60 * 1000)) * 10 * 60 * 1000
  const weekBoundary = Math.ceil((end - 7 * 24 * hour) / hour) * hour
  const windows = range === '24h'
    ? [{ resolution: '1m', from: end - 24 * hour, to: end }]
    : [
      ...(range === '30d' ? [{ resolution: '1h', from: end - 30 * 24 * hour, to: weekBoundary }] : []),
      { resolution: '10m', from: range === '30d' ? weekBoundary : end - 7 * 24 * hour, to: recentBoundary },
      { resolution: '1m', from: recentBoundary, to: end },
    ]
  return windows.map(({ resolution, from, to }) => new URLSearchParams({
    task_id: taskId, node_id: nodeId, resolution, from: new Date(from).toISOString(), to: new Date(to).toISOString(),
  }).toString())
}
