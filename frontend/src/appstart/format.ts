export function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  const s = ms / 1000
  return `${s.toFixed(s < 10 ? 2 : 1)} s`
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
