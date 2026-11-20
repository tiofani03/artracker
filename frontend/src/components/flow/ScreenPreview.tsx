import { useEffect, useState } from 'react'
import { flowApi } from '../../api'
import { fetchChunkPayloads } from '../../replay/parseChunk'
import { screenFrameData } from '../../replay/screen'

type Preview = { src: string; aspect: number; version: string } | null

// Decoded screenshots by project|platform|version|screen, so going back to a screen does not download its chunk again.
const cache = new Map<string, Preview>()

async function loadPreview(projectId: number, screen: string, platform: string, appVersion: string, signal: AbortSignal) {
  const { background: bg } = await flowApi.screenPreview(projectId, { screen, platform, app_version: appVersion })
  if (!bg) return null
  const { payloads } = await fetchChunkPayloads([bg.url], signal)
  const data = screenFrameData(payloads[0], bg.frame_index)
  if (!data) return null
  return { src: `data:image/jpeg;base64,${data}`, aspect: bg.vw / bg.vh || 0.5, version: bg.app_version }
}

/** A screenshot of the screen, reused from the heatmap, so readers who don't know the screen names can see the page. */
export default function ScreenPreview({
  projectId,
  screen,
  platform,
  appVersion,
  name,
}: {
  projectId: number
  screen: string
  platform: string
  appVersion: string
  name: string
}) {
  const key = `${projectId}|${platform}|${appVersion}|${screen}`
  // Bumped when a load finishes; the preview itself is read from the cache during render.
  const [, setLoaded] = useState('')
  const [open, setOpen] = useState(false)

  useEffect(() => {
    if (cache.has(key)) return
    const ctrl = new AbortController()
    loadPreview(projectId, screen, platform, appVersion, ctrl.signal)
      .catch(() => null)
      .then((preview) => {
        if (ctrl.signal.aborted) return
        cache.set(key, preview)
        setLoaded(key)
      })
    return () => ctrl.abort()
  }, [key, projectId, screen, platform, appVersion])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  if (!cache.has(key)) return <div className="fl-shot fl-shot--loading" aria-label="Loading screenshot" />
  const p = cache.get(key)
  if (!p) {
    return (
      <p className="fl-shot__none">
        No screenshot for this screen yet. Screenshots come from sessions recorded with screenshot replay.
      </p>
    )
  }
  const alt = `Screenshot of ${name}`
  return (
    <figure className="fl-shot">
      <button type="button" className="fl-shot__btn" onClick={() => setOpen(true)} title="Show larger">
        <img src={p.src} alt={alt} style={{ aspectRatio: String(p.aspect) }} />
      </button>
      <figcaption>
        Screenshot from a recorded session{p.version ? `, version ${p.version}` : ''}. Click to enlarge.
      </figcaption>
      {open && (
        <div className="fl-shot__overlay" role="dialog" aria-modal="true" aria-label={alt} onClick={() => setOpen(false)}>
          <img src={p.src} alt={alt} style={{ aspectRatio: String(p.aspect) }} />
          <button type="button" className="fl-shot__close" onClick={() => setOpen(false)} autoFocus>
            Close
          </button>
        </div>
      )}
    </figure>
  )
}
