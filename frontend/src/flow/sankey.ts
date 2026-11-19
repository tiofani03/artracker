import type { FlowLink, FlowNode } from '../api'

export type LaidNode = FlowNode & { x: number; y: number; h: number; sessions: number }
/** sy/ty are the y of the band centre at the source/target node; w is the band width. */
export type LaidLink = FlowLink & { sy: number; ty: number; w: number }

export const NODE_WIDTH = 14
export const NODE_GAP = 10

/** 0 for ordinary screens, 1 for the folded "Other" bucket, 2 for the Exit/Start terminals. */
function rank(screen: string): number {
  if (screen === 'Exit' || screen === 'Start') return 2
  if (screen === 'Other') return 1
  return 0
}

/**
 * Lays out a path-explorer Sankey: one column per step, columns evenly spaced across
 * `width`, node height proportional to max(incoming, outgoing) sessions, link bands
 * stacked inside their source/target nodes. All columns are top-aligned and none
 * exceeds `height`.
 */
export function layoutSankey(
  nodes: FlowNode[],
  links: FlowLink[],
  width: number,
  height: number,
): { nodes: LaidNode[]; links: LaidLink[] } {
  if (nodes.length === 0) return { nodes: [], links: [] }

  const incoming = new Map<string, number>()
  const outgoing = new Map<string, number>()
  for (const l of links) {
    outgoing.set(l.source, (outgoing.get(l.source) ?? 0) + l.sessions)
    incoming.set(l.target, (incoming.get(l.target) ?? 0) + l.sessions)
  }
  const sessionsOf = (id: string) => Math.max(incoming.get(id) ?? 0, outgoing.get(id) ?? 0)

  const steps = [...new Set(nodes.map((n) => n.step))].sort((a, b) => a - b)
  const columns = steps.map((step) =>
    nodes
      .filter((n) => n.step === step)
      .sort((a, b) => {
        const byRank = rank(a.screen) - rank(b.screen)
        if (byRank !== 0) return byRank
        return sessionsOf(b.id) - sessionsOf(a.id) || a.screen.localeCompare(b.screen)
      }),
  )

  // One scale for the whole chart, tight enough that the fullest column fits.
  let scale = Infinity
  for (const col of columns) {
    const total = col.reduce((s, n) => s + sessionsOf(n.id), 0)
    if (total <= 0) continue
    const room = Math.max(0, height - NODE_GAP * (col.length - 1))
    scale = Math.min(scale, room / total)
  }
  if (!Number.isFinite(scale)) scale = 0

  const colStep = columns.length > 1 ? Math.max(0, width - NODE_WIDTH) / (columns.length - 1) : 0
  const laid: LaidNode[] = []
  columns.forEach((col, ci) => {
    let y = 0
    for (const n of col) {
      const sessions = sessionsOf(n.id)
      const h = sessions * scale
      laid.push({ ...n, x: ci * colStep, y, h, sessions })
      y += h + NODE_GAP
    }
  })
  const byId = new Map(laid.map((n) => [n.id, n]))
  const order = new Map(laid.map((n, i) => [n.id, i]))

  // Stack bands inside each node: outgoing by target position, incoming by source position.
  const valid = links.filter((l) => byId.has(l.source) && byId.has(l.target))
  const sy = new Map<FlowLink, number>()
  const ty = new Map<FlowLink, number>()
  for (const n of laid) {
    let off = n.y
    const outs = valid
      .filter((l) => l.source === n.id)
      .sort((a, b) => byId.get(a.target)!.y - byId.get(b.target)!.y || order.get(a.target)! - order.get(b.target)!)
    for (const l of outs) {
      const w = l.sessions * scale
      sy.set(l, off + w / 2)
      off += w
    }
    off = n.y
    const ins = valid
      .filter((l) => l.target === n.id)
      .sort((a, b) => byId.get(a.source)!.y - byId.get(b.source)!.y || order.get(a.source)! - order.get(b.source)!)
    for (const l of ins) {
      const w = l.sessions * scale
      ty.set(l, off + w / 2)
      off += w
    }
  }

  return {
    nodes: laid,
    links: valid.map((l) => ({ ...l, sy: sy.get(l)!, ty: ty.get(l)!, w: l.sessions * scale })),
  }
}
