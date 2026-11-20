// Phases that hold app code running before the SDK's init/configure call. The startup profiler only starts at
// init, so this time has no function detail. Moving init earlier moves that code into a profiled phase.
// pre_main (dyld) and content_providers run before any app code can call init, so they are not listed.
const PRE_INIT_PHASES = new Set(['process_to_init', 'app_on_create_pre_init', 'pre_init'])

/** The pre-init phase name when it is the largest median phase, else null. */
export function slowPreInitPhase(phases: { name: string; median_ms: number }[]): string | null {
  let top: { name: string; median_ms: number } | null = null
  for (const p of phases) if (!top || p.median_ms > top.median_ms) top = p
  return top && top.median_ms > 0 && PRE_INIT_PHASES.has(top.name) ? top.name : null
}
