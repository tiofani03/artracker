import { useState, useRef, useMemo } from 'react'
import type { ReplayChunkDetail, ReplaySessionItem, SessionPerfResponse, TelemetryEvent } from '../../api'
import type { ReplayModel } from '../../replay/model'
import type { ExportFormat, ExportOptions, ExportProgress, ExportRangeMode } from '../../replay/exportTypes'
import { runReplayExport } from '../../replay/exportController'
import type { RefreshUrls } from '../../utils/screenChunkSource'
import Modal from '../Modal'

type Props = {
  open: boolean
  onClose: () => void
  session: ReplaySessionItem
  model: ReplayModel
  chunks: ReplayChunkDetail[]
  timelineEvents: TelemetryEvent[]
  perf?: SessionPerfResponse | null
  currentTimeMs: number
  durationMs: number
  baseTimestampMs: number
  refreshUrls: RefreshUrls
}

function msToMmSs(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000))
  const m = Math.floor(totalSec / 60)
  const s = totalSec % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

function mmSsToMs(str: string): number {
  const parts = str.trim().split(':')
  if (parts.length === 2) {
    const m = parseInt(parts[0], 10) || 0
    const s = parseInt(parts[1], 10) || 0
    return (m * 60 + s) * 1000
  }
  const s = parseInt(str, 10) || 0
  return s * 1000
}

export default function DownloadReplayModal({
  open,
  onClose,
  session,
  model,
  chunks,
  timelineEvents,
  perf = null,
  currentTimeMs,
  durationMs,
  baseTimestampMs,
  refreshUrls,
}: Props) {
  const hasVisual = model.kind !== 'none'

  const [format, setFormat] = useState<ExportFormat>(hasVisual ? 'video' : 'offline-html')
  const [rangeMode, setRangeMode] = useState<ExportRangeMode>('full')
  const [clipStartInput, setClipStartInput] = useState('00:00')
  const [clipEndInput, setClipEndInput] = useState(msToMmSs(durationMs))
  const [videoFps, setVideoFps] = useState(10)
  const [theme, setTheme] = useState<'light' | 'dark'>('dark')

  const [progress, setProgress] = useState<ExportProgress | null>(null)
  const [errorMsg, setErrorMsg] = useState<string | null>(null)
  const abortCtrlRef = useRef<AbortController | null>(null)

  const isExporting = progress !== null && progress.phase !== 'done' && progress.phase !== 'error'

  // Error marker offset if any
  const firstErrorOffsetMs = useMemo(() => {
    for (const ev of timelineEvents) {
      if (ev.level === 'error' || ev.kind === 'crash') {
        const ts = Date.parse(ev.event_timestamp)
        if (!Number.isNaN(ts)) {
          return Math.max(0, ts - baseTimestampMs)
        }
      }
    }
    return null
  }, [timelineEvents, baseTimestampMs])

  const setPresetRange = (startMs: number, endMs: number) => {
    const s = Math.max(0, Math.min(startMs, durationMs))
    const e = Math.max(s, Math.min(endMs, durationMs))
    setRangeMode('clip')
    setClipStartInput(msToMmSs(s))
    setClipEndInput(msToMmSs(e))
  }

  const handleStartExport = async () => {
    setErrorMsg(null)
    const startMs = rangeMode === 'clip' ? mmSsToMs(clipStartInput) : 0
    const endMs = rangeMode === 'clip' ? mmSsToMs(clipEndInput) : durationMs

    if (rangeMode === 'clip' && startMs >= endMs) {
      setErrorMsg('Clip start time must be before end time.')
      return
    }

    const options: ExportOptions = {
      format,
      rangeMode,
      clipStartMs: startMs,
      clipEndMs: endMs,
      videoFps,
      wireframeTheme: theme,
    }

    const controller = new AbortController()
    abortCtrlRef.current = controller

    try {
      await runReplayExport({
        options,
        session,
        model,
        chunks,
        timelineEvents,
        perf,
        baseTimestampMs,
        durationMs,
        refreshUrls,
        onProgress: (p) => setProgress(p),
        signal: controller.signal,
      })
      setTimeout(() => {
        onClose()
        setProgress(null)
      }, 1000)
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        setProgress(null)
        return
      }
      setErrorMsg(err instanceof Error ? err.message : 'Export failed')
      setProgress(null)
    } finally {
      abortCtrlRef.current = null
    }
  }

  const handleCancelExport = () => {
    if (abortCtrlRef.current) {
      abortCtrlRef.current.abort()
      abortCtrlRef.current = null
    }
    setProgress(null)
  }

  return (
    <Modal
      open={open}
      onClose={() => {
        if (!isExporting) onClose()
      }}
      title="Download Session Replay"
      description={`Export session ${session.id.slice(0, 12)} to your local device`}
    >
      <div className="replay-download-modal">
        {errorMsg && (
          <div className="dash-note dash-note--error" style={{ marginBottom: 12 }}>
            {errorMsg}
          </div>
        )}

        {isExporting ? (
          <div className="replay-download-progress">
            <div className="replay-download-progress__msg">{progress?.message || 'Processing export…'}</div>
            <div className="replay-download-progress__bar">
              <div
                className="replay-download-progress__fill"
                style={{ width: `${progress?.percent || 0}%` }}
              />
            </div>
            <div className="replay-download-progress__sub">
              <span>{progress?.percent || 0}%</span>
              <span>100% Client-Side Export</span>
            </div>
            <div style={{ marginTop: 16, display: 'flex', justifyContent: 'center' }}>
              <button type="button" className="dash-btn" onClick={handleCancelExport}>
                Cancel Export
              </button>
            </div>
          </div>
        ) : (
          <>
            {/* 1. Format Selection */}
            <div className="replay-download-section">
              <label className="replay-download-label">Choose Export Format</label>
              <div className="replay-format-grid">
                {/* Format 1: Video */}
                <div
                  className={`replay-format-card${format === 'video' ? ' is-selected' : ''}${!hasVisual ? ' is-disabled' : ''}`}
                  onClick={() => hasVisual && setFormat('video')}
                >
                  <div className="replay-format-card__head">
                    <span className="replay-format-card__title">Video (.mp4 / .webm)</span>
                    <span className="badge">Video</span>
                  </div>
                  <p className="replay-format-card__desc">
                    Ready-to-share video of user actions, clicks, and screens.
                  </p>
                  {!hasVisual && <span className="dash-list__sub" style={{ color: 'var(--color-warn)' }}>No visual recording in this session</span>}
                </div>

                {/* Format 2: Images Only (.zip) */}
                <div
                  className={`replay-format-card${format === 'images-zip' ? ' is-selected' : ''}${!hasVisual ? ' is-disabled' : ''}`}
                  onClick={() => hasVisual && setFormat('images-zip')}
                >
                  <div className="replay-format-card__head">
                    <span className="replay-format-card__title">Images Only (.zip)</span>
                    <span className="badge">ZIP</span>
                  </div>
                  <p className="replay-format-card__desc">
                    Sequential screenshots / keyframes + manifest.json.
                  </p>
                  {!hasVisual && <span className="dash-list__sub" style={{ color: 'var(--color-warn)' }}>No visual recording in this session</span>}
                </div>

                {/* Format 3: Standalone Offline Player */}
                <div
                  className={`replay-format-card${format === 'offline-html' ? ' is-selected' : ''}`}
                  onClick={() => setFormat('offline-html')}
                >
                  <div className="replay-format-card__head">
                    <span className="replay-format-card__title">Offline Player (.html)</span>
                    <span className="badge">Standalone</span>
                  </div>
                  <p className="replay-format-card__desc">
                    Single interactive HTML file playable offline with timeline & scrubber.
                  </p>
                </div>

                {/* Format 4: Raw Bundle */}
                <div
                  className={`replay-format-card${format === 'raw-bundle' ? ' is-selected' : ''}`}
                  onClick={() => setFormat('raw-bundle')}
                >
                  <div className="replay-format-card__head">
                    <span className="replay-format-card__title">Raw Bundle (.zip)</span>
                    <span className="badge">Debug</span>
                  </div>
                  <p className="replay-format-card__desc">
                    Diagnostic package with raw chunk JSONs, timeline, and perf metrics.
                  </p>
                </div>
              </div>
            </div>

            {/* 2. Duration / Range Selection */}
            <div className="replay-download-section">
              <label className="replay-download-label">Duration Scope</label>
              <div className="dash-seg" style={{ marginBottom: 10 }}>
                <button
                  type="button"
                  className={`dash-seg__btn${rangeMode === 'full' ? ' is-active' : ''}`}
                  onClick={() => setRangeMode('full')}
                >
                  Full Session ({msToMmSs(durationMs)})
                </button>
                <button
                  type="button"
                  className={`dash-seg__btn${rangeMode === 'clip' ? ' is-active' : ''}`}
                  onClick={() => setRangeMode('clip')}
                >
                  Custom Clip
                </button>
              </div>

              {rangeMode === 'clip' && (
                <div className="replay-clip-box">
                  <div className="replay-clip-inputs">
                    <div>
                      <span className="dash-list__sub">Start</span>
                      <input
                        type="text"
                        className="dash-input"
                        style={{ width: 80, textAlign: 'center' }}
                        value={clipStartInput}
                        onChange={(e) => setClipStartInput(e.target.value)}
                        placeholder="00:00"
                      />
                    </div>
                    <span style={{ padding: '0 8px', alignSelf: 'center', color: '#64748b' }}>to</span>
                    <div>
                      <span className="dash-list__sub">End</span>
                      <input
                        type="text"
                        className="dash-input"
                        style={{ width: 80, textAlign: 'center' }}
                        value={clipEndInput}
                        onChange={(e) => setClipEndInput(e.target.value)}
                        placeholder="00:00"
                      />
                    </div>
                  </div>

                  <div className="replay-presets">
                    <button
                      type="button"
                      className="dash-btn dash-btn--compact"
                      onClick={() => setPresetRange(currentTimeMs - 15000, currentTimeMs + 15000)}
                    >
                      Playhead ±15s
                    </button>
                    <button
                      type="button"
                      className="dash-btn dash-btn--compact"
                      onClick={() => setPresetRange(currentTimeMs, durationMs)}
                    >
                      Playhead to End
                    </button>
                    {firstErrorOffsetMs !== null && (
                      <button
                        type="button"
                        className="dash-btn dash-btn--compact"
                        onClick={() => setPresetRange(firstErrorOffsetMs - 10000, firstErrorOffsetMs + 10000)}
                      >
                        Around Error
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* 3. Format Specific Options */}
            {format === 'video' && (
              <div className="replay-download-section">
                <label className="replay-download-label">Video Settings</label>
                <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
                  <span className="dash-list__sub">Frame Rate:</span>
                  <select
                    className="dash-select"
                    style={{ width: 110 }}
                    value={videoFps}
                    onChange={(e) => setVideoFps(Number(e.target.value))}
                  >
                    <option value={5}>5 fps</option>
                    <option value={10}>10 fps (Default)</option>
                    <option value={15}>15 fps</option>
                  </select>
                </div>
              </div>
            )}

            {model.kind === 'mobile' && (
              <div className="replay-download-section">
                <label className="replay-download-label">Wireframe Theme</label>
                <div className="dash-seg">
                  <button
                    type="button"
                    className={`dash-seg__btn${theme === 'dark' ? ' is-active' : ''}`}
                    onClick={() => setTheme('dark')}
                  >
                    Dark Theme
                  </button>
                  <button
                    type="button"
                    className={`dash-seg__btn${theme === 'light' ? ' is-active' : ''}`}
                    onClick={() => setTheme('light')}
                  >
                    Light Theme
                  </button>
                </div>
              </div>
            )}

            {/* Footer action buttons */}
            <div className="replay-download-footer">
              <button type="button" className="dash-btn" onClick={onClose}>
                Cancel
              </button>
              <button
                type="button"
                className="dash-btn dash-btn--primary"
                onClick={handleStartExport}
              >
                Download
              </button>
            </div>
          </>
        )}
      </div>
    </Modal>
  )
}
