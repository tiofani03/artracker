import type { Platform } from '../../api'
import { phaseLabel } from '../../appstart/format'

const SNIPPETS: Record<'android' | 'ios', string> = {
  android: `ARTrackerConfig.Builder(/* … */)
    .appStart(AppStartConfig(profiling = true, profileSampleRate = 0.1))`,
  ios: `config.enableStartupProfiling = true
config.startupProfileSampleRate = 0.1`,
}

/** Shown in place of the functions table when no profiled cold start matches the filters. */
export function ProfilingSetup({ platform }: { platform: Platform }) {
  const ios = platform === 'ios'
  return (
    <div className="as-hint">
      <p>
        <strong>No profiled cold starts here.</strong> The phases above show where the time goes; this table shows which
        functions use it, and it needs the startup profiler, which is off by default. Turn it on in the SDK config:
      </p>
      <pre className="as-hint__code">{SNIPPETS[ios ? 'ios' : 'android']}</pre>
      <ul className="as-hint__list">
        <li>Only cold starts are profiled. To test, force-stop the app and open it again.</li>
        <li>
          The sample rate is the share of cold starts that get profiled (0.1 = 10%). Use 1.0 in debug builds to profile
          every one.
        </li>
        <li>
          For {ios ? 'release builds, upload the dSYMs' : 'minified builds, upload the R8 / ProGuard mapping'} so function
          names are readable.
        </li>
      </ul>
    </div>
  )
}

/** Shown under the phase bar when the phase before SDK init is the slowest one. */
export function PreInitHint({ phase, platform }: { phase: string; platform: Platform }) {
  const ios = platform === 'ios'
  return (
    <p className="as-hint as-hint--inline">
      <strong>{phaseLabel(phase)}</strong> is the slowest phase. Code that runs before{' '}
      <code>{ios ? 'ARTracker.configure' : 'ARTracker.init'}</code> (
      {ios ? 'dependency injection setup' : <code>startKoin</code>}, other SDKs) is counted here, and the profiler cannot
      break it down by function. Call it first in <code>{ios ? 'didFinishLaunching' : 'Application.onCreate'}</code> to see that code
      in the functions table.
    </p>
  )
}
