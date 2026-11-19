import { describe, expect, it } from 'vitest'
import type { FlowLink, FlowNode } from '../api'
import { layoutSankey } from './sankey'

const n = (step: number, screen: string): FlowNode => ({ id: `${step}:${screen}`, step, screen })
const l = (source: string, target: string, sessions: number): FlowLink => ({ source, target, sessions })

// Home(100) -> Cart(60), Search(30), Exit(10); Cart -> Pay(40), Exit(20); Search -> Exit(30)
const nodes: FlowNode[] = [
  n(0, 'Home'),
  n(1, 'Exit'), n(1, 'Search'), n(1, 'Other'), n(1, 'Cart'), // Other has no links: zero-height node
  n(2, 'Pay'), n(2, 'Exit'),
]
const links: FlowLink[] = [
  l('0:Home', '1:Cart', 60),
  l('0:Home', '1:Search', 30),
  l('0:Home', '1:Exit', 10),
  l('1:Cart', '2:Pay', 40),
  l('1:Cart', '2:Exit', 20),
  l('1:Search', '2:Exit', 30),
]
const W = 600
const H = 400

describe('layoutSankey', () => {
  const out = layoutSankey(nodes, links, W, H)
  const byId = new Map(out.nodes.map((x) => [x.id, x]))

  it('returns an empty layout for no nodes', () => {
    expect(layoutSankey([], [], W, H)).toEqual({ nodes: [], links: [] })
  })

  it('puts one column per step with evenly spaced x inside the width', () => {
    const xs = [0, 1, 2].map((s) => out.nodes.find((x) => x.step === s)!.x)
    expect(xs[0]).toBe(0)
    expect(xs[1] - xs[0]).toBeCloseTo(xs[2] - xs[1])
    for (const node of out.nodes) {
      const sameStep = out.nodes.filter((o) => o.step === node.step)
      expect(sameStep.every((o) => o.x === node.x)).toBe(true)
    }
    expect(xs[2]).toBeLessThanOrEqual(W)
  })

  it('sizes nodes proportionally to max(incoming, outgoing) sessions', () => {
    expect(byId.get('0:Home')!.sessions).toBe(100)
    expect(byId.get('1:Cart')!.sessions).toBe(60)
    expect(byId.get('2:Exit')!.sessions).toBe(50)
    const k = byId.get('0:Home')!.h / 100
    expect(k).toBeGreaterThan(0)
    for (const node of out.nodes) expect(node.h).toBeCloseTo(node.sessions * k)
  })

  it('sorts a column by sessions desc with Other then Exit last', () => {
    const col = out.nodes.filter((x) => x.step === 1).sort((a, b) => a.y - b.y)
    expect(col.map((x) => x.screen)).toEqual(['Cart', 'Search', 'Other', 'Exit'])
  })

  it('sorts Other before Exit/Start at the end of a column', () => {
    const r = layoutSankey(
      [n(0, 'A'), n(1, 'Exit'), n(1, 'Other'), n(1, 'B')],
      [l('0:A', '1:Exit', 50), l('0:A', '1:Other', 20), l('0:A', '1:B', 5)],
      W, H,
    )
    const col = r.nodes.filter((x) => x.step === 1).sort((a, b) => a.y - b.y)
    expect(col.map((x) => x.screen)).toEqual(['B', 'Other', 'Exit'])
  })

  it('makes link widths leaving a node equal its height', () => {
    for (const node of out.nodes) {
      const leaving = out.links.filter((x) => x.source === node.id)
      if (leaving.length === 0) continue
      expect(leaving.reduce((s, x) => s + x.w, 0)).toBeCloseTo(node.h)
    }
  })

  it('keeps link bands inside their source and target nodes', () => {
    for (const link of out.links) {
      const s = byId.get(link.source)!
      const t = byId.get(link.target)!
      expect(link.sy - link.w / 2).toBeGreaterThanOrEqual(s.y - 1e-9)
      expect(link.sy + link.w / 2).toBeLessThanOrEqual(s.y + s.h + 1e-9)
      expect(link.ty - link.w / 2).toBeGreaterThanOrEqual(t.y - 1e-9)
      expect(link.ty + link.w / 2).toBeLessThanOrEqual(t.y + t.h + 1e-9)
    }
  })

  it('stacks outgoing bands in target order', () => {
    const fromHome = out.links.filter((x) => x.source === '0:Home').sort((a, b) => a.sy - b.sy)
    expect(fromHome.map((x) => x.target)).toEqual(['1:Cart', '1:Search', '1:Exit'])
  })

  it('never exceeds the height in any column', () => {
    for (const step of [0, 1, 2]) {
      const col = out.nodes.filter((x) => x.step === step)
      const bottom = Math.max(...col.map((x) => x.y + x.h))
      expect(Math.min(...col.map((x) => x.y))).toBeGreaterThanOrEqual(0)
      expect(bottom).toBeLessThanOrEqual(H + 1e-9)
    }
    for (const node of out.nodes) expect(node.h).toBeLessThanOrEqual(H)
  })

  it('does not overlap nodes within a column', () => {
    const col = out.nodes.filter((x) => x.step === 1).sort((a, b) => a.y - b.y)
    for (let i = 1; i < col.length; i++) expect(col[i].y).toBeGreaterThan(col[i - 1].y + col[i - 1].h)
  })

  it('handles a single column without NaN', () => {
    const r = layoutSankey([n(0, 'Home')], [], W, H)
    expect(r.nodes).toHaveLength(1)
    expect(Number.isFinite(r.nodes[0].x)).toBe(true)
    expect(Number.isFinite(r.nodes[0].h)).toBe(true)
  })
})
