import type { PerfSample } from '../api'
import type { SeriesPoint } from './tracks'

/** A reading is "current" until this many sample intervals have passed without a newer one. */
const STALE_INTERVALS = 3
/** Samples further apart than this many intervals are not part of the same run (app was in the background). */
const RUN_GAP_INTERVALS = 2.5

const HIGH_CPU_PCT = 80
const HIGH_CPU_MIN_SAMPLES = 3
const LOW_FPS_RATIO = 0.5
const LOW_FPS_MIN_SAMPLES = 2
const JANK_BURST = 5
const DEFAULT_REFRESH_HZ = 60

/** The sample the playhead is on (latest with t <= absTs), or null before the first / long after the last. */
export function perfValueAt(samples: PerfSample[], absTs: number, intervalMs: number): PerfSample | null {
  let found: PerfSample | null = null
  for (const s of samples) {
    if (s.t <= absTs) found = s
    else break
  }
  if (!found) return null
  return absTs - found.t > STALE_INTERVALS * intervalMs ? null : found
}

/** Series points for one metric; samples that did not measure it leave a gap rather than a zero. */
export function toSeries(samples: PerfSample[], key: 'cpu' | 'mem_mb' | 'fps', startMs: number): SeriesPoint[] {
  const points: SeriesPoint[] = []
  for (const s of samples) {
    const v = s[key]
    if (typeof v === 'number') points.push({ tMs: s.t - startMs, v })
  }
  return points
}

export type PerfBreach = {
  kind: 'high_cpu' | 'low_fps' | 'jank_burst'
  startMs: number
  endMs: number
  peak: number
}

/** Maximal runs of adjacent samples satisfying `pred`, split wherever the time gap exceeds the run limit. */
function runsOf(samples: PerfSample[], pred: (s: PerfSample) => boolean, intervalMs: number): PerfSample[][] {
  const runs: PerfSample[][] = []
  let current: PerfSample[] = []
  for (const s of samples) {
    const last = current[current.length - 1]
    if (!pred(s) || (last && s.t - last.t > RUN_GAP_INTERVALS * intervalMs)) {
      if (current.length) runs.push(current)
      current = []
    }
    if (pred(s)) current.push(s)
  }
  if (current.length) runs.push(current)
  return runs
}

/** Threshold breaches (session-profiling-design §6.4), ordered by start time. */
export function detectBreaches(
  samples: PerfSample[],
  startMs: number,
  intervalMs: number,
  refreshHz: number = DEFAULT_REFRESH_HZ,
): PerfBreach[] {
  const out: PerfBreach[] = []
  const span = (run: PerfSample[]) => ({
    startMs: run[0].t - startMs,
    endMs: run[run.length - 1].t - startMs + intervalMs,
  })

  for (const run of runsOf(samples, (s) => (s.cpu ?? -1) >= HIGH_CPU_PCT, intervalMs)) {
    if (run.length >= HIGH_CPU_MIN_SAMPLES) {
      out.push({ kind: 'high_cpu', ...span(run), peak: Math.max(...run.map((s) => s.cpu as number)) })
    }
  }

  const lowFps = refreshHz * LOW_FPS_RATIO
  for (const run of runsOf(samples, (s) => typeof s.fps === 'number' && s.fps < lowFps, intervalMs)) {
    if (run.length >= LOW_FPS_MIN_SAMPLES) {
      out.push({ kind: 'low_fps', ...span(run), peak: Math.min(...run.map((s) => s.fps as number)) })
    }
  }

  for (const s of samples) {
    if ((s.jank ?? 0) >= JANK_BURST) {
      out.push({ kind: 'jank_burst', ...span([s]), peak: s.jank as number })
    }
  }

  return out.sort((a, b) => a.startMs - b.startMs)
}

export type PerfSummary = {
  cpuPeak?: number
  cpuAvg?: number
  memPeak?: number
  jankTotal: number
  frozenTotal: number
}

export function summarizePerf(samples: PerfSample[]): PerfSummary {
  const cpus = samples.flatMap((s) => (typeof s.cpu === 'number' ? [s.cpu] : []))
  const mems = samples.flatMap((s) => (typeof s.mem_mb === 'number' ? [s.mem_mb] : []))
  return {
    cpuPeak: cpus.length ? Math.max(...cpus) : undefined,
    cpuAvg: cpus.length ? cpus.reduce((a, b) => a + b, 0) / cpus.length : undefined,
    memPeak: mems.length ? Math.max(...mems) : undefined,
    jankTotal: samples.reduce((n, s) => n + (s.jank ?? 0), 0),
    frozenTotal: samples.reduce((n, s) => n + (s.frozen ?? 0), 0),
  }
}

/** Short label and sentence for a breach, shared by the timeline markers, the event list and the Performance tab. */
export function describeBreach(b: PerfBreach, refreshHz: number = DEFAULT_REFRESH_HZ): { badge: string; text: string } {
  const secs = Math.max(1, Math.round((b.endMs - b.startMs) / 1000))
  if (b.kind === 'high_cpu') {
    return { badge: 'High CPU', text: `CPU ≥ ${HIGH_CPU_PCT}% for ${secs}s (peak ${Math.round(b.peak)}%)` }
  }
  if (b.kind === 'low_fps') {
    return {
      badge: 'Low FPS',
      text: `FPS below ${Math.round(refreshHz * LOW_FPS_RATIO)} for ${secs}s (lowest ${Math.round(b.peak)})`,
    }
  }
  return { badge: 'Jank burst', text: `${Math.round(b.peak)} janky frames in ${secs}s` }
}

/** Top of the RAM track: the session's own peak plus headroom, so growth is visible even on a large-RAM device. */
export function memTrackMax(samples: PerfSample[]): number {
  let peak = 0
  for (const s of samples) if (typeof s.mem_mb === 'number' && s.mem_mb > peak) peak = s.mem_mb
  return Math.max(1, peak * 1.2)
}
