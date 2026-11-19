import { describe, expect, it } from 'vitest'
import type { FlowEdge } from '../api'
import { layoutMap, MAP_NODE_H, MAP_NODE_W } from './map'

const e = (source: string, target: string, sessions: number): FlowEdge => ({ source, target, sessions })

const EDGES: FlowEdge[] = [
  e('Start', 'Home', 8),
  e('Start', 'Login', 2),
  e('Login', 'Home', 2),
  e('Home', 'Cart', 6),
  e('Home', 'Search', 3),
  e('Cart', 'Home', 1),
  e('Cart', 'Pay', 4),
  e('Pay', 'Exit', 4),
  e('Search', 'Exit', 3),
]

describe('layoutMap', () => {
  it('places Start first, screens by hop distance, and Exit in its own last column', () => {
    const m = layoutMap(EDGES, 1)
    const layer = Object.fromEntries(m.nodes.map((n) => [n.screen, n.layer]))
    // Home is one hop from Start, but Login → Home would stay inside one column, so Home moves right.
    expect(layer).toEqual({ Start: 0, Login: 1, Home: 2, Cart: 3, Search: 3, Pay: 4, Exit: 5 })
  })

  it('never leaves an edge inside one column', () => {
    const m = layoutMap(EDGES, 1)
    const layer = Object.fromEntries(m.nodes.map((n) => [n.screen, n.layer]))
    for (const x of m.edges) expect(layer[x.source]).not.toBe(layer[x.target])
  })

  it('orders a column by sessions, busiest on top', () => {
    const m = layoutMap(EDGES, 1)
    const col = m.nodes.filter((n) => n.layer === 3).sort((a, b) => a.y - b.y)
    expect(col.map((n) => n.screen)).toEqual(['Cart', 'Search'])
  })

  it('gives each node the sessions that reached it (Start: the sessions that left it)', () => {
    const m = layoutMap(EDGES, 1)
    const s = Object.fromEntries(m.nodes.map((n) => [n.screen, n.sessions]))
    expect(s.Start).toBe(10)
    expect(s.Home).toBe(11)
    expect(s.Exit).toBe(7)
  })

  it('flags edges that go back or stay in the same column', () => {
    const m = layoutMap(EDGES, 1)
    const back = m.edges.filter((x) => x.back).map((x) => `${x.source}>${x.target}`)
    expect(back).toEqual(['Cart>Home'])
  })

  it('scales edge width by sessions and draws every edge as a path', () => {
    const m = layoutMap(EDGES, 1)
    const w = Object.fromEntries(m.edges.map((x) => [`${x.source}>${x.target}`, x.width]))
    expect(w['Start>Home']).toBeGreaterThan(w['Cart>Home'])
    for (const x of m.edges) expect(x.d).toMatch(/^M[\d.-]+,[\d.-]+ C/)
  })

  it('drops edges below the minimum and the screens left without edges', () => {
    const m = layoutMap(EDGES, 3)
    expect(m.edges.every((x) => x.sessions >= 3)).toBe(true)
    expect(m.nodes.map((n) => n.screen).sort()).toEqual(['Cart', 'Exit', 'Home', 'Pay', 'Search', 'Start'])
  })

  it('puts a screen unreachable from Start after its predecessors', () => {
    const m = layoutMap([e('Start', 'A', 5), e('B', 'C', 4), e('C', 'Exit', 4)], 1)
    const layer = Object.fromEntries(m.nodes.map((n) => [n.screen, n.layer]))
    expect(layer.B).toBe(1)
    expect(layer.C).toBe(2)
    expect(layer.Exit).toBe(3)
  })

  it('keeps nodes inside the reported size and returns nothing for no edges', () => {
    const m = layoutMap(EDGES, 1)
    for (const n of m.nodes) {
      expect(n.x).toBeGreaterThanOrEqual(0)
      expect(n.y).toBeGreaterThanOrEqual(0)
      expect(n.x + MAP_NODE_W).toBeLessThanOrEqual(m.width)
      expect(n.y + MAP_NODE_H).toBeLessThanOrEqual(m.height)
    }
    expect(layoutMap([], 1)).toEqual({ nodes: [], edges: [], width: 0, height: 0 })
  })
})
