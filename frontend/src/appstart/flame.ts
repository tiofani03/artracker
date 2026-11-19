import type { SymbolicatedProfile } from '../api'

// start/end are in ms on the profile's own sample clock (sample.t).
export type FlameBox = { depth: number; start: number; end: number; frame: number }

// Backend weighting rule: sample i spans [t_i, t_i + min(t_{i+1} - t_i, MAX_GAP_MS)),
// the last sample spans interval_ms, and a gap <= 0 weighs 0.
const MAX_GAP_MS = 100

function sampleWeights(p: SymbolicatedProfile): number[] {
  const { samples, interval_ms } = p
  return samples.map((s, i) => {
    if (i === samples.length - 1) return interval_ms
    const gap = samples[i + 1].t - s.t
    return gap <= 0 ? 0 : Math.min(gap, MAX_GAP_MS)
  })
}

function prefixEqual(a: number[], b: number[], n: number): boolean {
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return false
  return true
}

type OpenBox = FlameBox & { i0: number }

export function layoutFlame(p: SymbolicatedProfile): FlameBox[] {
  const { samples, stacks } = p
  const weights = sampleWeights(p)
  const boxes: FlameBox[] = []
  let maxDepth = 0
  for (const s of samples) maxDepth = Math.max(maxDepth, stacks[s.s].length)

  for (let d = 0; d < maxDepth; d++) {
    let open: OpenBox | null = null
    const flush = () => {
      if (open && open.end > open.start) boxes.push({ depth: open.depth, start: open.start, end: open.end, frame: open.frame })
    }
    for (let i = 0; i < samples.length; i++) {
      const stack = stacks[samples[i].s]
      const f = d < stack.length ? stack[d] : -1
      const end = samples[i].t + weights[i]
      if (open && f === open.frame && prefixEqual(stacks[samples[open.i0].s], stack, d)) {
        open.end = Math.max(open.end, end)
      } else {
        flush()
        open = f >= 0 ? { depth: d, start: samples[i].t, end, frame: f, i0: i } : null
      }
    }
    flush()
  }
  return boxes
}

export function boxStats(p: SymbolicatedProfile, box: FlameBox): { totalMs: number; selfMs: number } {
  const weights = sampleWeights(p)
  let totalMs = 0
  let selfMs = 0
  // Samples are sorted by t (the backend guarantees it), so the box covers a contiguous run of them.
  p.samples.forEach((s, i) => {
    if (s.t < box.start || s.t >= box.end) return
    const stack = p.stacks[s.s]
    if (stack.length <= box.depth || stack[box.depth] !== box.frame) return
    totalMs += weights[i]
    if (stack.length === box.depth + 1) selfMs += weights[i]
  })
  return { totalMs, selfMs }
}
