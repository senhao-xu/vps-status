import { describe, expect, it } from 'vitest'

import type { Node, ProbeTask } from '../../api/contracts'
import { parseProbeTarget, probeAssignmentWrite, probeSelection, visibleProbeNodes } from './probeConfiguration'
import { probeTarget } from './probePresentation'

const node = (id: string, group: string): Node => ({ id, name: `Node ${id}`, group, hidden: false, homepageVisible: false, hostname: '', os: '', kernel: '', architecture: '', virtualization: '', connected: false, online: false, lastReportAt: null, report: null })
const nodes = [node('a', ''), node('b', 'all'), node('c', 'g'), node('d', '全组')]
const task: ProbeTask = { id: 'task', name: 'Check', host: 'example.com', port: 443, intervalSeconds: 60, autoJoin: true, updatedAt: '2026-10-15T08:00:00Z', nodeIds: ['a'], latest: [] }

// Native-dialog interaction and request behavior are exercised against the
// embedded application; these tests cover the parsing and selection contracts.
describe('TCP configuration target', () => {
  it.each([
    ['example.com:443', 'example.com', 443], ['localhost:80', 'localhost', 80],
    ['127.0.0.1:1', '127.0.0.1', 1], ['10.0.0.1:65535', '10.0.0.1', 65535],
    ['[::1]:443', '::1', 443], ['[2001:db8::1]:443', '2001:db8::1', 443],
    ['[2001:0db8:0:0:0:0:0:1]:443', '2001:0db8:0:0:0:0:0:1', 443],
  ])('parses and formats %s', (input, host, port) => {
    const parsed = parseProbeTarget(input)
    expect(parsed).toEqual({ host, port })
    if (parsed) expect(probeTarget(parsed)).toBe(input)
  })
  it.each(['', 'example.com', 'example.com:', ':443', 'example.com:0', 'example.com:65536', 'example.com:-1', 'example.com:1.2', 'example.com:1e2', ' example.com:443', 'example.com:443 ', 'exam ple.com:443', 'https://example.com:443', 'user@example.com:443', 'example.com:443/path', 'example.com:443?x', 'example.com:443#x', 'example.com\\:443', '::1:443', '[invalid]:443', '[1::2::3]:443', '[127.0.0.1]:443', '[]:443', '-host:443', 'host..com:443', 'host:443\n'])('rejects %j', (input) => {
    expect(parseProbeTarget(input)).toBeNull()
  })
})

describe('TCP configuration node selection', () => {
  it('defaults new tasks to all existing nodes, preserves edit assignments and includes actual refreshed assignments', () => {
    expect([...probeSelection(null, nodes, new Map())]).toEqual(['a', 'b', 'c', 'd'])
    expect([...probeSelection(task, nodes, new Map())]).toEqual(['a'])
    const overrides = new Map([['a', false], ['c', true]])
    expect([...probeSelection({ ...task, nodeIds: ['a', 'b'] }, nodes, overrides)]).toEqual(['b', 'c'])
    expect([...probeSelection({ ...task, nodeIds: ['a', 'b', 'd'] }, nodes, new Map([...overrides, ['b', false]]))]).toEqual(['c', 'd'])
  })
  it('filters arbitrary groups without colliding with the all-groups sentinel', () => {
    expect(visibleProbeNodes(nodes, null, '')).toHaveLength(4)
    expect(visibleProbeNodes(nodes, '', '').map((item) => item.id)).toEqual(['a'])
    expect(visibleProbeNodes(nodes, 'all', '').map((item) => item.id)).toEqual(['b'])
    expect(visibleProbeNodes(nodes, 'g', ' NODE C ').map((item) => item.id)).toEqual(['c'])
    expect(visibleProbeNodes(nodes, '全组', '全组').map((item) => item.id)).toEqual(['d'])
  })
  it('retains hidden choices when only shown nodes are deselected', () => {
    const shown = visibleProbeNodes(nodes, 'all', '')
    const overrides = new Map(shown.map((item) => [item.id, false]))
    expect([...probeSelection(null, nodes, overrides)]).toEqual(['a', 'c', 'd'])
  })
})

describe('TCP configuration assignment submission', () => {
  it('creates with the complete chosen set, including default-all and explicit empty selection', () => {
    expect(probeAssignmentWrite(null, nodes, new Map())).toEqual({ node_ids: ['a', 'b', 'c', 'd'] })
    expect(probeAssignmentWrite(null, nodes, new Map(nodes.map(({ id }) => [id, false])))).toEqual({ node_ids: [] })
  })

  it('omits assignment replacement when an edit leaves selection untouched, even with a stale snapshot', () => {
    // Enrollment may have assigned a node absent from both cached task and nodes.
    expect(probeAssignmentWrite(task, nodes, new Map())).toEqual({})
    expect(probeAssignmentWrite({ ...task, nodeIds: ['a', 'new-node'] }, [...nodes, node('new-node', '')], new Map())).toEqual({})
  })

  it('submits only explicit choices and preserves unrelated assignments across refresh and filtering', () => {
    const overrides = new Map([['a', false], ['c', true]])
    const expected = { node_ids_add: ['c'], node_ids_remove: ['a'] }
    expect(probeAssignmentWrite(task, nodes, overrides)).toEqual(expected)
    expect(probeAssignmentWrite({ ...task, nodeIds: ['a', 'b', 'new-node'] }, [...nodes, node('new-node', '')], overrides)).toEqual(expected)
    // Selecting a shown node already assigned still expresses the explicit choice.
    expect(probeAssignmentWrite(task, nodes, new Map([['a', true]]))).toEqual({ node_ids_add: ['a'] })
  })

  it('does not re-add a deleted node but retains explicit removals as idempotent changes', () => {
    expect(probeAssignmentWrite(task, nodes, new Map([['deleted-node', true], ['removed-node', false]]))).toEqual({ node_ids_remove: ['removed-node'] })
  })
})
