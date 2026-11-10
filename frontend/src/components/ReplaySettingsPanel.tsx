import { useState } from 'react'
import { projectApi, type Project } from '../api'
import { useToast } from './Toast'

type Props = { project: Project; canManage: boolean; onChange?: (p: Project) => void }

/** Per-project switch for screenshot-mode mobile replay. Admins and owners can flip it; others see it read-only. */
export default function ReplaySettingsPanel({ project, canManage, onChange }: Props) {
  const toast = useToast()
  const [enabled, setEnabled] = useState(project.replay_screenshot_enabled !== false)
  const [busy, setBusy] = useState(false)

  const toggle = async () => {
    if (!canManage || busy) return
    const next = !enabled
    setEnabled(next)
    setBusy(true)
    try {
      const updated = await projectApi.setReplaySettings(project.id, { screenshot_enabled: next })
      onChange?.(updated)
      toast(next ? 'Screenshot replay enabled' : 'Screenshot replay disabled')
    } catch (err) {
      setEnabled(!next)
      toast(err instanceof Error ? err.message : 'Failed to update replay settings', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="dash-card" aria-labelledby="replay-settings-title">
      <div className="dash-card__head">
        <div>
          <h2 id="replay-settings-title" className="dash-card__title">
            Screenshot replay
          </h2>
          <p className="dash-card__desc">
            Lets the mobile SDKs of {project.name} upload real screen images for session replay. The app must also
            enable <code>SCREENSHOT</code> mode in its SDK config; turning this off stops uploads for every app version
            without a release.
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label="Screenshot replay"
          className={`dash-switch${enabled ? ' is-on' : ''}`}
          disabled={!canManage || busy}
          onClick={() => void toggle()}
        >
          <span className="dash-switch__knob" />
        </button>
      </div>
      {!canManage && <p className="dash-note">Only admins and owners can change this setting.</p>}
    </section>
  )
}
