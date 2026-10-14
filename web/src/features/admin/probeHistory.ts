import { apiRequest } from '../../api/client'
import { decodeProbePoints, type ProbePoint } from '../../api/contracts'
import { probeHistoryQueries, type ProbeHistoryRange } from './probePresentation'

export async function loadProbeHistory(taskId: string, nodeId: string, range: ProbeHistoryRange, signal: AbortSignal): Promise<ProbePoint[]> {
  const queries = probeHistoryQueries(taskId, nodeId, range, new Date())
  // One failed tier rejects the whole history rather than plotting a partial
  // window that could be mistaken for missing samples. All requests share cancellation.
  const series = await Promise.all(queries.map((query) => apiRequest(`/api/v1/admin/telemetry/probe?${query}`, decodeProbePoints, { signal })))
  const byTime = new Map<number, ProbePoint>()
  for (const point of series.flat()) byTime.set(Date.parse(point.bucketStart), point)
  return [...byTime.values()].sort((a, b) => Date.parse(a.bucketStart) - Date.parse(b.bucketStart))
}
