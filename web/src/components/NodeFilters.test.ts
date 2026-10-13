import { describe, expect, it } from 'vitest'

import { filterNodes } from './NodeFilters'

describe('node filtering', () => {
  const nodes = [{ name: 'Edge Tokyo', group: 'JP' }, { name: 'Storage', group: 'Home' }, { name: 'Singapore', group: 'Asia' }]
  it('matches trimmed case-insensitive names and groups and intersects the group filter', () => {
    expect(filterNodes(nodes, '', '  tOKYo ')).toEqual([nodes[0]])
    expect(filterNodes(nodes, '', 'asia')).toEqual([nodes[2]])
    expect(filterNodes(nodes, 'Home', 'storage')).toEqual([nodes[1]])
    expect(filterNodes(nodes, 'JP', 'storage')).toEqual([])
    expect(filterNodes(nodes, '', '')).toEqual(nodes)
  })
})
