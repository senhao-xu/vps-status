import { reduceNodeEvent, type LiveEvent, type Node } from '../../api/contracts'

// This buffer exists only while a dashboard snapshot is in flight. Replaying
// its events prevents a slow configuration request from rolling back live state.
export function createNodeRefreshBuffer() {
  const events: LiveEvent[] = []
  return {
    record(event: LiveEvent): boolean {
      if (events.length >= 512) return false
      events.push(event)
      return true
    },
    replay(nodes: Node[]): Node[] {
      return events.reduce(reduceNodeEvent, nodes)
    },
  }
}
