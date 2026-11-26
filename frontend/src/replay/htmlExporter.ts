import type { ReplaySessionItem, SessionPerfResponse, TelemetryEvent } from '../api'
import type { MobileFrame, TouchPoint } from './model'
import type { ScreenFrameMeta, ScreenTouch } from './screen'

export type HtmlExportOptions = {
  session: ReplaySessionItem
  mode: 'screen' | 'mobile' | 'web' | 'none'
  startAbsMs: number
  endAbsMs: number
  timelineEvents?: TelemetryEvent[]
  perf?: SessionPerfResponse | null
  screenFrames?: ScreenFrameMeta[]
  screenTouches?: ScreenTouch[]
  screenImagesMap?: Map<number, string> // ts -> base64
  mobileFrames?: MobileFrame[]
  mobileTouches?: TouchPoint[]
  onProgress?: (percent: number, message: string) => void
  signal?: AbortSignal
}

function escapeJsonForScript(jsonStr: string): string {
  return jsonStr.replace(/<\/script/gi, '<\\/script')
}

/**
 * Builds a standalone, self-contained interactive HTML player file (.html).
 */
export async function exportOfflineHtmlPlayer(opts: HtmlExportOptions): Promise<Blob> {
  const {
    session,
    mode,
    startAbsMs,
    endAbsMs,
    timelineEvents = [],
    perf = null,
    screenFrames = [],
    screenTouches = [],
    screenImagesMap = new Map(),
    mobileFrames = [],
    mobileTouches = [],
    onProgress,
    signal,
  } = opts

  if (signal?.aborted) {
    throw new DOMException('Aborted', 'AbortError')
  }

  onProgress?.(20, 'Preparing offline session payload…')

  // Screen images dictionary
  const screenImagesObj: Record<number, string> = {}
  for (const [ts, b64] of screenImagesMap) {
    screenImagesObj[ts] = b64
  }

  const payload = {
    session,
    mode,
    startAbsMs,
    endAbsMs,
    timelineEvents,
    perf,
    screenFrames,
    screenTouches,
    screenImages: screenImagesObj,
    mobileFrames,
    mobileTouches,
  }

  const payloadJson = escapeJsonForScript(JSON.stringify(payload))
  onProgress?.(60, 'Assembling player scripts and stylesheet…')

  const htmlContent = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="Content-Security-Policy" content="default-src 'self' 'unsafe-inline' data: blob:;">
  <title>Session Replay - ${session.id}</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #090d16;
      color: #f1f5f9;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      height: 100vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
      user-select: none;
    }
    .player-header {
      height: 48px;
      background: #0f172a;
      border-bottom: 1px solid rgba(255, 255, 255, 0.08);
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0 16px;
      font-size: 13px;
    }
    .player-header__title { font-weight: 600; color: #38bdf8; display: flex; align-items: center; gap: 8px; }
    .player-header__meta { color: #94a3b8; font-size: 12px; }
    .badge { padding: 2px 8px; border-radius: 999px; font-size: 11px; background: rgba(56, 189, 248, 0.15); color: #38bdf8; border: 1px solid rgba(56, 189, 248, 0.3); }
    .player-body {
      flex: 1;
      display: flex;
      overflow: hidden;
    }
    .stage-container {
      flex: 1;
      display: flex;
      align-items: center;
      justify-content: center;
      background: #030712;
      position: relative;
    }
    canvas {
      box-shadow: 0 12px 36px rgba(0, 0, 0, 0.7);
      border-radius: 8px;
      background: #090d16;
    }
    .events-panel {
      width: 320px;
      background: #0b1120;
      border-left: 1px solid rgba(255, 255, 255, 0.08);
      display: flex;
      flex-direction: column;
    }
    .events-panel__head {
      padding: 12px 16px;
      font-weight: 600;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: #94a3b8;
      border-bottom: 1px solid rgba(255, 255, 255, 0.06);
    }
    .events-list {
      flex: 1;
      overflow-y: auto;
    }
    .event-item {
      padding: 10px 14px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.04);
      cursor: pointer;
      font-size: 12px;
      transition: background 0.15s;
    }
    .event-item:hover { background: rgba(255, 255, 255, 0.05); }
    .event-item.is-active { background: rgba(56, 189, 248, 0.12); border-left: 3px solid #38bdf8; }
    .event-item__time { font-size: 11px; color: #64748b; margin-bottom: 2px; }
    .event-item__name { font-weight: 500; color: #e2e8f0; }
    .event-item--error .event-item__name { color: #f87171; }
    .player-controls {
      height: 64px;
      background: #0f172a;
      border-top: 1px solid rgba(255, 255, 255, 0.08);
      display: flex;
      flex-direction: column;
      padding: 6px 16px;
    }
    .timeline-bar {
      height: 12px;
      background: rgba(255, 255, 255, 0.08);
      border-radius: 6px;
      position: relative;
      cursor: pointer;
      margin-bottom: 6px;
    }
    .timeline-bar__fill {
      height: 100%;
      background: #38bdf8;
      border-radius: 6px;
      width: 0%;
      pointer-events: none;
    }
    .timeline-bar__marker {
      position: absolute;
      top: 0;
      width: 3px;
      height: 100%;
      background: #f87171;
    }
    .controls-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
    }
    .controls-left { display: flex; align-items: center; gap: 12px; }
    button.btn {
      background: rgba(255, 255, 255, 0.08);
      border: 1px solid rgba(255, 255, 255, 0.15);
      color: #f1f5f9;
      padding: 6px 12px;
      border-radius: 6px;
      cursor: pointer;
      font-size: 12px;
      font-weight: 500;
    }
    button.btn:hover { background: rgba(255, 255, 255, 0.14); }
    .time-display { font-family: monospace; font-size: 13px; color: #94a3b8; }
  </style>
</head>
<body>
  <div class="player-header">
    <div class="player-header__title">
      <span>● ARTracker Offline Player</span>
      <span class="badge">${mode.toUpperCase()}</span>
    </div>
    <div class="player-header__meta">
      Session: <strong>${session.id}</strong> | User: <strong>${session.user_id || 'anonymous'}</strong>
    </div>
  </div>

  <div class="player-body">
    <div class="stage-container">
      <canvas id="stage" width="402" height="874"></canvas>
    </div>
    <div class="events-panel">
      <div class="events-panel__head">Timeline Events (${timelineEvents.length})</div>
      <div class="events-list" id="eventsList"></div>
    </div>
  </div>

  <div class="player-controls">
    <div class="timeline-bar" id="timelineBar">
      <div class="timeline-bar__fill" id="timelineFill"></div>
    </div>
    <div class="controls-row">
      <div class="controls-left">
        <button type="button" class="btn" id="playBtn">Play</button>
        <button type="button" class="btn" id="speedBtn">1x</button>
        <span class="time-display" id="timeDisplay">00:00 / 00:00</span>
      </div>
    </div>
  </div>

  <script id="replay-payload" type="application/json">${payloadJson}</script>

  <script>
    (function() {
      const data = JSON.parse(document.getElementById('replay-payload').textContent);
      const canvas = document.getElementById('stage');
      const ctx = canvas.getContext('2d');
      const playBtn = document.getElementById('playBtn');
      const speedBtn = document.getElementById('speedBtn');
      const timeDisplay = document.getElementById('timeDisplay');
      const timelineBar = document.getElementById('timelineBar');
      const timelineFill = document.getElementById('timelineFill');
      const eventsList = document.getElementById('eventsList');

      let isPlaying = false;
      let speed = 1;
      let currentMs = 0;
      const durationMs = Math.max(1000, data.endAbsMs - data.startAbsMs);

      function formatTime(ms) {
        const sec = Math.floor(ms / 1000);
        const m = Math.floor(sec / 60);
        const s = sec % 60;
        return String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0');
      }

      // Preload decoded images for screen mode
      const imageBitmaps = {};
      if (data.mode === 'screen') {
        Object.entries(data.screenImages || {}).forEach(([ts, b64]) => {
          const img = new Image();
          img.src = 'data:image/jpeg;base64,' + b64;
          imageBitmaps[ts] = img;
        });
      }

      // Render timeline events
      (data.timelineEvents || []).forEach((ev) => {
        const item = document.createElement('div');
        item.className = 'event-item' + (ev.level === 'error' ? ' event-item--error' : '');
        const timeParsed = Date.parse(ev.event_timestamp);
        const relTime = Number.isNaN(timeParsed) ? 0 : Math.max(0, timeParsed - data.startAbsMs);
        item.innerHTML = '<div class="event-item__time">' + formatTime(relTime) + '</div>' +
          '<div class="event-item__name">' + (ev.name || ev.message || 'Event') + '</div>';
        item.onclick = () => seekTo(relTime);
        eventsList.appendChild(item);

        // Add marker dot to timeline
        const marker = document.createElement('div');
        marker.className = 'timeline-bar__marker';
        marker.style.left = (Math.min(1, relTime / durationMs) * 100) + '%';
        timelineBar.appendChild(marker);
      });

      function render() {
        const absTime = data.startAbsMs + currentMs;
        ctx.fillStyle = '#090d16';
        ctx.fillRect(0, 0, canvas.width, canvas.height);

        if (data.mode === 'screen') {
          // Find closest frame <= absTime
          let activeFrame = null;
          for (const f of data.screenFrames) {
            if (f.ts <= absTime) activeFrame = f;
            else break;
          }
          if (activeFrame && imageBitmaps[activeFrame.ts]) {
            const img = imageBitmaps[activeFrame.ts];
            if (img.complete && img.naturalWidth > 0) {
              const scale = Math.min(canvas.width / activeFrame.w, canvas.height / activeFrame.h);
              const dw = Math.round(activeFrame.w * scale);
              const dh = Math.round(activeFrame.h * scale);
              const dx = Math.round((canvas.width - dw) / 2);
              const dy = Math.round((canvas.height - dh) / 2);
              ctx.drawImage(img, dx, dy, dw, dh);
            }
          }
        } else {
          ctx.fillStyle = '#38bdf8';
          ctx.font = 'bold 16px sans-serif';
          ctx.textAlign = 'center';
          ctx.fillText(data.mode.toUpperCase() + ' REPLAY', canvas.width / 2, canvas.height / 2);
        }

        timeDisplay.textContent = formatTime(currentMs) + ' / ' + formatTime(durationMs);
        timelineFill.style.width = ((currentMs / durationMs) * 100) + '%';
      }

      function seekTo(ms) {
        currentMs = Math.max(0, Math.min(ms, durationMs));
        render();
      }

      timelineBar.onclick = (e) => {
        const rect = timelineBar.getBoundingClientRect();
        const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
        seekTo(pct * durationMs);
      };

      playBtn.onclick = () => {
        isPlaying = !isPlaying;
        playBtn.textContent = isPlaying ? 'Pause' : 'Play';
        if (isPlaying && currentMs >= durationMs) seekTo(0);
      };

      speedBtn.onclick = () => {
        speed = speed === 1 ? 2 : speed === 2 ? 4 : 1;
        speedBtn.textContent = speed + 'x';
      };

      let lastRaf = performance.now();
      function loop(now) {
        const dt = now - lastRaf;
        lastRaf = now;
        if (isPlaying) {
          currentMs += dt * speed;
          if (currentMs >= durationMs) {
            currentMs = durationMs;
            isPlaying = false;
            playBtn.textContent = 'Play';
          }
          render();
        }
        requestAnimationFrame(loop);
      }
      render();
      requestAnimationFrame(loop);
    })();
  </script>
</body>
</html>`

  onProgress?.(100, 'HTML player ready!')
  return new Blob([htmlContent], { type: 'text/html;charset=utf-8' })
}
