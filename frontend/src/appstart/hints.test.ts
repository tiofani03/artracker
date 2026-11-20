import { describe, expect, it } from 'vitest'
import { slowPreInitPhase } from './hints'

const p = (name: string, ms: number) => ({ name, median_ms: ms })

describe('slowPreInitPhase', () => {
  it('returns the Android pre-init phase when it is the largest', () => {
    const phases = [p('process_to_init', 1200), p('application_on_create', 358), p('activity_create', 179), p('first_draw', 549)]
    expect(slowPreInitPhase(phases)).toBe('process_to_init')
  })

  it('returns null when another phase is larger', () => {
    const phases = [p('process_to_init', 300), p('application_on_create', 900), p('first_draw', 200)]
    expect(slowPreInitPhase(phases)).toBeNull()
  })

  it('covers the Android 15+ and iOS pre-init phases', () => {
    expect(slowPreInitPhase([p('bind_application', 100), p('app_on_create_pre_init', 800), p('first_draw', 200)])).toBe(
      'app_on_create_pre_init',
    )
    expect(slowPreInitPhase([p('pre_main', 200), p('pre_init', 700), p('did_finish_launching', 300)])).toBe('pre_init')
  })

  it('ignores phases the app cannot move before SDK init', () => {
    // dyld work and ContentProviders run before any app code can call init.
    expect(slowPreInitPhase([p('pre_main', 900), p('pre_init', 100)])).toBeNull()
    expect(slowPreInitPhase([p('content_providers', 900), p('app_on_create_pre_init', 100)])).toBeNull()
  })

  it('returns null for no phases or a zero-length pre-init phase', () => {
    expect(slowPreInitPhase([])).toBeNull()
    expect(slowPreInitPhase([p('process_to_init', 0)])).toBeNull()
  })
})
