import { useEffect, useState } from 'react'
import { heatmapApi, type Project } from '../api'
import { MAX_EXCLUDED, MAX_NAME_LEN, normalizeExcluded } from '../heatmap/excluded'
import { useToast } from './Toast'

type Props = { project: Project; canManage: boolean }

/** Screens excluded from tap-heatmap collection. Admins and owners can edit; others see it read-only. */
export default function HeatmapSettingsPanel({ project, canManage }: Props) {
  const toast = useToast()
  const [rule, setRule] = useState('')
  const [screens, setScreens] = useState<string[]>([])
  const [draft, setDraft] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    heatmapApi
      .settings(project.id)
      .then((s) => {
        if (!alive) return
        setRule(s.sensitive_rule)
        setScreens(s.excluded_screens)
      })
      .catch((err) => alive && setLoadError(err instanceof Error ? err.message : 'Failed to load heatmap settings'))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [project.id])

  const add = () => {
    const name = draft.trim()
    if (!name) return
    if (name.length > MAX_NAME_LEN) return toast(`Screen names are limited to ${MAX_NAME_LEN} characters`, 'error')
    const next = normalizeExcluded([...screens, name])
    if (next.length > MAX_EXCLUDED) return toast(`At most ${MAX_EXCLUDED} screens can be excluded`, 'error')
    setScreens(next)
    setDraft('')
  }

  const save = async () => {
    if (!canManage || busy) return
    setBusy(true)
    try {
      const s = await heatmapApi.saveSettings(project.id, normalizeExcluded(screens))
      setScreens(s.excluded_screens)
      toast('Excluded screens saved. Their heatmap data was removed.')
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Failed to save heatmap settings', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="dash-panel-stack">
      <section className="dash-card dash-card--compact" aria-labelledby="heatmap-settings-title">
        <div className="dash-card__head">
          <div>
            <h2 id="heatmap-settings-title" className="dash-card__title">
              Tap Heatmap Exclusions
            </h2>
            <p className="dash-card__desc">
              Screens in <strong>{project.name}</strong> that never contribute to tap heatmaps.
            </p>
          </div>
        </div>

        {loading ? (
          <p className="dash-list__sub">Loading…</p>
        ) : loadError ? (
          <p className="dash-note" role="alert">{loadError}</p>
        ) : (
          <>
            <p className="dash-list__sub" style={{ lineHeight: 1.6 }}>
              Always excluded: <code>{rule}</code>
              <br />
              Screens matching this are never collected.
            </p>

            {canManage && (
              <form
                style={{ display: 'flex', gap: '8px', marginTop: 'var(--space-xs)' }}
                onSubmit={(e) => {
                  e.preventDefault()
                  add()
                }}
              >
                <input
                  className="dash-input"
                  aria-label="Screen name to exclude"
                  placeholder="Screen name, e.g. CheckoutScreen"
                  maxLength={MAX_NAME_LEN}
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                />
                <button type="submit" className="dash-btn" disabled={!draft.trim()}>
                  Add
                </button>
              </form>
            )}

            {screens.length === 0 ? (
              <p className="dash-list__sub" style={{ marginTop: 'var(--space-xs)' }}>No screens excluded.</p>
            ) : (
              <ul style={{ listStyle: 'none', padding: 0, margin: 'var(--space-xs) 0 0' }}>
                {screens.map((s) => (
                  <li key={s} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', padding: '4px 0' }}>
                    <code>{s}</code>
                    {canManage && (
                      <button
                        type="button"
                        className="dash-btn dash-btn--danger"
                        aria-label={`Remove ${s}`}
                        onClick={() => setScreens(screens.filter((x) => x !== s))}
                      >
                        Remove
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            )}

            {canManage ? (
              <button type="button" className="dash-btn dash-btn--primary" style={{ marginTop: 'var(--space-xs)' }} disabled={busy} onClick={() => void save()}>
                {busy ? 'Saving…' : 'Save'}
              </button>
            ) : (
              <p className="dash-note" style={{ marginTop: 'var(--space-xs)' }}>
                Only organization admins and owners can change excluded screens.
              </p>
            )}
          </>
        )}
      </section>
    </div>
  )
}
