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
    <div className="dash-panel-stack">
      <section className="dash-card dash-card--compact" aria-labelledby="replay-settings-title">
        <div className="dash-card__head">
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
              <h2 id="replay-settings-title" className="dash-card__title">
                Mobile Screenshot Replay
              </h2>
              <span className={`badge ${enabled ? 'badge--active' : 'badge--revoked'}`}>
                {enabled ? 'Active' : 'Disabled'}
              </span>
            </div>
            <p className="dash-card__desc">
              Controls visual session replay image ingestion for <strong>{project.name}</strong>.
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

        <p className="dash-list__sub" style={{ lineHeight: 1.6 }}>
          Allows native Android and iOS mobile SDKs to capture and upload periodic UI frames to reconstruct user sessions in the replay theater. Turning this off acts as an immediate killswitch, rejecting screenshot uploads from all app versions without requiring a client store release.
        </p>

        {!canManage && (
          <p className="dash-note" style={{ marginTop: 'var(--space-xs)' }}>
            Only organization admins and owners can modify project replay settings.
          </p>
        )}

        <div className="replay-features-grid">
          <div className="replay-feature-card">
            <h3 className="replay-feature-card__title">On-Device Privacy Masking</h3>
            <p className="replay-feature-card__desc">
              Password inputs, card numbers, and views with sensitive tags are redacted directly on the device before any image data is compressed or uploaded.
            </p>
          </div>

          <div className="replay-feature-card">
            <h3 className="replay-feature-card__title">Bandwidth &amp; Battery Optimized</h3>
            <p className="replay-feature-card__desc">
              Screenshots are captured at an adaptive 2 fps rate, compressed with gzip chunks, and synced over WiFi or unmetered connections to preserve battery.
            </p>
          </div>
        </div>
      </section>
    </div>
  )
}
