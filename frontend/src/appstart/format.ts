export function formatMs(ms: number): string {
  const rounded = Math.round(ms)
  if (rounded < 1000) return `${rounded} ms`
  const s = rounded / 1000
  const twoDecimals = s.toFixed(2)
  return `${Number(twoDecimals) < 10 ? twoDecimals : s.toFixed(1)} s`
}

const PHASE_LABELS: Record<string, string> = {
  process_to_init: 'Process → SDK init',
  bind_application: 'Bind application',
  content_providers: 'Content providers',
  app_on_create_pre_init: 'onCreate before SDK init',
  application_on_create: 'Application.onCreate',
  activity_create: 'Activity create',
  activity_start: 'Activity start',
  first_draw: 'First draw',
  pre_main: 'Pre-main',
  pre_init: 'main → SDK init',
  did_finish_launching: 'didFinishLaunching',
  scene_activation: 'Scene activation',
  foreground_transition: 'Foreground transition',
}

export function phaseLabel(name: string): string {
  return PHASE_LABELS[name] ?? name.replaceAll('_', ' ')
}

/** Share of all starts, rounded; "<1%" keeps a small but non-zero type visible. */
export function shareLabel(count: number, total: number): string {
  if (total === 0 || count === 0) return '0%'
  const pct = (100 * count) / total
  return pct < 1 ? '<1%' : `${Math.round(pct)}%`
}
