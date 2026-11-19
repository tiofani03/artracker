import type { FlowEdge } from '../api'

/** Node box size and spacing of the whole-app map (SVG user units). */
export const MAP_NODE_W = 156
export const MAP_NODE_H = 40
const COL_GAP = 64
const ROW_GAP = 18
const PAD = 12
/** Room under the lowest node for back edges, which loop underneath. */
const BACK_ROOM = 56
const MIN_EDGE_W = 1.5
const MAX_EDGE_W = 10

const START = 'Start'
const EXIT = 'Exit'

export type MapNode = { screen: string; layer: number; x: number; y: number; sessions: number }
export type MapEdge = FlowEdge & { back: boolean; width: number; d: string }
export type MapLayout = { nodes: MapNode[]; edges: MapEdge[]; width: number; height: number }

const r = (n: number) => Math.round(n * 10) / 10

/**
 * Lays the screen graph out in columns: Start first, each screen at its shortest hop distance from Start
 * (screens not reachable from Start go one column after their furthest placed predecessor), and Exit in a
 * column of its own at the end. A column is ordered by sessions, busiest on top. Edges below minSessions
 * are dropped, with the screens they leave edgeless. Edges that do not go right (back to an earlier or the
 * same column) are flagged and loop underneath the nodes.
 */
export function layoutMap(all: FlowEdge[], minSessions: number): MapLayout {
  const edges = all.filter((e) => e.sessions >= minSessions && e.source !== e.target)
  if (edges.length === 0) return { nodes: [], edges: [], width: 0, height: 0 }

  const names = new Set<string>()
  const out = new Map<string, string[]>()
  const incoming = new Map<string, number>()
  const outgoing = new Map<string, number>()
  const preds = new Map<string, string[]>()
  for (const e of edges) {
    names.add(e.source)
    names.add(e.target)
    out.set(e.source, [...(out.get(e.source) ?? []), e.target])
    preds.set(e.target, [...(preds.get(e.target) ?? []), e.source])
    incoming.set(e.target, (incoming.get(e.target) ?? 0) + e.sessions)
    outgoing.set(e.source, (outgoing.get(e.source) ?? 0) + e.sessions)
  }

  // Shortest hop distance from Start; Exit is placed last, separately.
  const layer = new Map<string, number>()
  if (names.has(START)) {
    layer.set(START, 0)
    const queue = [START]
    while (queue.length) {
      const cur = queue.shift()!
      for (const next of out.get(cur) ?? []) {
        if (next === EXIT || layer.has(next)) continue
        layer.set(next, layer.get(cur)! + 1)
        queue.push(next)
      }
    }
  }
  // Unreachable screens: one column after their furthest placed predecessor (repeat until stable).
  const rest = [...names].filter((n) => n !== EXIT && !layer.has(n))
  for (let pass = 0; pass <= rest.length && rest.some((n) => !layer.has(n)); pass++) {
    for (const n of rest) {
      if (layer.has(n)) continue
      const placed = (preds.get(n) ?? []).filter((p) => layer.has(p)).map((p) => layer.get(p)!)
      if (placed.length) layer.set(n, Math.max(...placed) + 1)
      else if (pass === rest.length || !(preds.get(n) ?? []).some((p) => rest.includes(p))) layer.set(n, 1)
    }
  }
  for (const n of rest) if (!layer.has(n)) layer.set(n, 1)
  // An edge inside one column is unreadable: push its target one column right (bounded, so a
  // same-column cycle ends with one back edge instead of looping).
  for (let pass = 0; pass < names.size; pass++) {
    let moved = false
    for (const e of edges) {
      if (e.target === EXIT || e.source === EXIT) continue
      if (layer.get(e.source) === layer.get(e.target)) {
        layer.set(e.target, layer.get(e.source)! + 1)
        moved = true
      }
    }
    if (!moved) break
  }
  const lastScreenLayer = Math.max(0, ...[...layer.values()])
  if (names.has(EXIT)) layer.set(EXIT, lastScreenLayer + 1)

  const sessionsOf = (n: string) => (n === START ? (outgoing.get(n) ?? 0) : (incoming.get(n) ?? 0))
  const columns = new Map<number, string[]>()
  for (const n of names) columns.set(layer.get(n)!, [...(columns.get(layer.get(n)!) ?? []), n])
  const pos = new Map<string, { x: number; y: number }>()
  let rows = 0
  for (const [l, col] of columns) {
    col.sort((a, b) => sessionsOf(b) - sessionsOf(a) || a.localeCompare(b))
    col.forEach((n, i) => pos.set(n, { x: PAD + l * (MAP_NODE_W + COL_GAP), y: PAD + i * (MAP_NODE_H + ROW_GAP) }))
    rows = Math.max(rows, col.length)
  }
  const maxLayer = Math.max(...columns.keys())

  const nodes: MapNode[] = [...names].map((n) => ({ screen: n, layer: layer.get(n)!, ...pos.get(n)!, sessions: sessionsOf(n) }))
  const maxEdge = Math.max(...edges.map((e) => e.sessions))
  const bodyBottom = PAD + rows * MAP_NODE_H + (rows - 1) * ROW_GAP
  let hasBack = false
  const laidEdges: MapEdge[] = edges.map((e) => {
    const s = pos.get(e.source)!
    const t = pos.get(e.target)!
    const back = layer.get(e.source)! >= layer.get(e.target)!
    const width = r(MIN_EDGE_W + (MAX_EDGE_W - MIN_EDGE_W) * (e.sessions / maxEdge))
    let d: string
    if (back) {
      hasBack = true
      const x0 = s.x + MAP_NODE_W / 2
      const y0 = s.y + MAP_NODE_H
      const x1 = t.x + MAP_NODE_W / 2
      const y1 = t.y + MAP_NODE_H
      const low = Math.max(y0, y1) + BACK_ROOM * 0.75
      d = `M${r(x0)},${r(y0)} C${r(x0)},${r(low)} ${r(x1)},${r(low)} ${r(x1)},${r(y1)}`
    } else {
      const x0 = s.x + MAP_NODE_W
      const y0 = s.y + MAP_NODE_H / 2
      const x1 = t.x
      const y1 = t.y + MAP_NODE_H / 2
      const mx = (x0 + x1) / 2
      d = `M${r(x0)},${r(y0)} C${r(mx)},${r(y0)} ${r(mx)},${r(y1)} ${r(x1)},${r(y1)}`
    }
    return { ...e, back, width, d }
  })

  return {
    nodes,
    edges: laidEdges,
    width: PAD * 2 + (maxLayer + 1) * MAP_NODE_W + maxLayer * COL_GAP,
    height: bodyBottom + PAD + (hasBack ? BACK_ROOM : 0),
  }
}
