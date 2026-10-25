import type { Node, ProbeTask } from '../../api/contracts'

// Parse the complete authority before splitting it for the existing API.
export function parseProbeTarget(value: string): { host: string; port: number } | null {
  if (!value || /[\s/@?#\\]/u.test(value)) return null
  const match = /^(?:\[([^\]]+)\]|([^:[\]]+)):([0-9]+)$/u.exec(value)
  if (!match) return null
  const host = match[1] ?? match[2]
  const port = Number(match[3])
  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null
  if (match[1]) {
    if (!host.includes(':')) return null
    try { new URL(`http://[${host}]:${port}`) } catch { return null }
  } else if (host.length > 253 || !host.replace(/\.$/u, '').split('.').every((label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/iu.test(label))) return null
  return { host, port }
}

export function visibleProbeNodes(nodes: Node[], group: string | null, query: string): Node[] {
  const needle = query.trim().toLocaleLowerCase()
  return nodes.filter((node) => (group === null || node.group === group) && `${node.name} ${node.group}`.toLocaleLowerCase().includes(needle))
}

// Explicit choices win over refreshed assignments, including deselection of an
// auto-assigned node. Untouched assignments can arrive while an edit stays open.
export function probeSelection(task: ProbeTask | null, nodes: Node[], overrides: ReadonlyMap<string, boolean>): Set<string> {
  return new Set(nodes.filter((node) => overrides.get(node.id) ?? (task ? task.nodeIds.includes(node.id) : true)).map((node) => node.id))
}

type ProbeAssignmentWrite = { node_ids: string[] } | { node_ids_add?: string[]; node_ids_remove?: string[] }

// An edit carries only explicit choices, so enrollment after the snapshot remains assigned.
export function probeAssignmentWrite(task: ProbeTask | null, nodes: Node[], overrides: ReadonlyMap<string, boolean>): ProbeAssignmentWrite {
  if (!task) return { node_ids: [...probeSelection(null, nodes, overrides)] }
  const add = nodes.filter((node) => overrides.get(node.id) === true).map((node) => node.id)
  const remove = [...overrides].filter(([, selected]) => !selected).map(([id]) => id)
  return { ...(add.length ? { node_ids_add: add } : {}), ...(remove.length ? { node_ids_remove: remove } : {}) }
}
