import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import type { ReplayChunkDetail, ReplaySessionItem, SessionPerfResponse } from '../src/api'
import ReplayPlayer from '../src/components/ReplayPlayer'
import { ScreenReplayRenderer } from '../src/components/ScreenReplayRenderer'
import { ScreenReplaySession } from '../src/replay/screenSession'
import { useFillHeight } from '../src/replay/useFillHeight'
// Same stylesheet order as src/main.tsx, so tokens (spacing, colours) resolve exactly as in the app
import '../src/tokens.css'
import '../src/base.css'
import '../src/index.css'
import '../src/dashboard.css'

// Fixture: 40 solid-colour frames, 500 ms apart, 10 per chunk. The colour encodes the frame index, so a test
// can tell exactly which frame is on the canvas.
const BASE = 1_700_000_000_000
const FRAMES = 40
const PER_CHUNK = 10
const SPACING = 500
const WIDTH = 300
const HEIGHT = 500

type RGB = [number, number, number]
const colorOf = (i: number): RGB => [20 + ((i * 5) % 200), 235 - ((i * 5) % 200), 20 + ((i * 37) % 200)]

function bytesToBase64(bytes: Uint8Array): string {
  let s = ''
  for (let o = 0; o < bytes.length; o += 0x8000) s += String.fromCharCode(...bytes.subarray(o, o + 0x8000))
  return btoa(s)
}

async function canvasJpeg(draw: (ctx: CanvasRenderingContext2D) => void, size: number, quality: number): Promise<Uint8Array> {
  const c = document.createElement('canvas')
  c.width = c.height = size
  draw(c.getContext('2d')!)
  const blob = await new Promise<Blob>((resolve) => c.toBlob((b) => resolve(b!), 'image/jpeg', quality))
  return new Uint8Array(await blob.arrayBuffer())
}

const solidJpeg = (i: number) =>
  canvasJpeg(
    (ctx) => {
      const [r, g, b] = colorOf(i)
      ctx.fillStyle = `rgb(${r},${g},${b})`
      ctx.fillRect(0, 0, 16, 16)
    },
    16,
    1,
  )

const decodeOptions = { colorSpaceConversion: 'none', premultiplyAlpha: 'none' } as const

async function buildChunks(): Promise<{ chunks: unknown[]; jpegs: string[] }> {
  const jpegs = await Promise.all(Array.from({ length: FRAMES }, async (_, i) => bytesToBase64(await solidJpeg(i))))
  const chunks = Array.from({ length: FRAMES / PER_CHUNK }, (_, k) => ({
    format: 'artracker-screen-v1',
    platform: 'android',
    seq: k,
    frames: Array.from({ length: PER_CHUNK }, (_, j) => {
      const i = k * PER_CHUNK + j
      return { ts: BASE + i * SPACING, screen: i < 20 ? 'home' : 'detail', w: 720, h: 1600, vw: 360, vh: 800, mime: 'image/jpeg', data: jpegs[i], masked: 0 }
    }),
    touches:
      k === 0
        ? [
            { ts: BASE + 1000, x: 180, y: 400, kind: 'down' },
            { ts: BASE + 1100, x: 180, y: 400, kind: 'up' },
          ]
        : [],
  }))
  return { chunks, jpegs }
}

/** Perf mode: 30 minutes at 2 fps, ~80 KB of unique base64 per frame, generated when a chunk is fetched. */
async function perfSession(): Promise<ScreenReplaySession> {
  const noise = await canvasJpeg(
    (ctx) => {
      const img = ctx.createImageData(300, 300)
      for (let p = 0; p < img.data.length; p += 4) {
        img.data[p] = Math.random() * 255
        img.data[p + 1] = Math.random() * 255
        img.data[p + 2] = Math.random() * 255
        img.data[p + 3] = 255
      }
      ctx.putImageData(img, 0, 0)
    },
    300,
    0.75,
  )
  const chunkCount = 180
  const framesPerChunk = 20
  const unique = (n: number) => {
    const copy = new Uint8Array(noise.length + 4)
    copy.set(noise)
    copy.set([n & 255, (n >> 8) & 255, (n >> 16) & 255, 7], noise.length) // bytes after the JPEG end marker are ignored
    return bytesToBase64(copy)
  }
  const seqs = Array.from({ length: chunkCount }, (_, k) => k)
  return new ScreenReplaySession({
    seqs,
    sessionStartMs: BASE,
    chunkCreatedAtMs: new Map(seqs.map((k) => [k, BASE + (k + 1) * 10_000 + 500])),
    fetchChunk: async (seq) => ({
      format: 'artracker-screen-v1',
      platform: 'android',
      seq,
      frames: Array.from({ length: framesPerChunk }, (_, j) => ({
        ts: BASE + seq * 10_000 + j * 500,
        screen: 'perf',
        w: 300,
        h: 300,
        vw: 300,
        vh: 300,
        mime: 'image/jpeg',
        data: unique(seq * framesPerChunk + j),
        masked: 0,
      })),
      touches: [],
    }),
  })
}

let setPlayhead: (ts: number) => void = () => {}

function RendererApp({ session }: { session: ScreenReplaySession }) {
  const [ts, setTs] = useState(BASE)
  useEffect(() => {
    setPlayhead = setTs
  }, [])
  return <ScreenReplayRenderer session={session} currentTs={ts} width={WIDTH} height={HEIGHT} />
}

const sessionItem = {
  id: 'sess_e2e_12345678',
  project_id: 1,
  user_id: '',
  environment: 'test',
  start_time: new Date(BASE).toISOString(),
  end_time: null,
  duration_ms: FRAMES * SPACING - SPACING,
  chunk_count: FRAMES / PER_CHUNK,
  event_count: FRAMES,
  total_size_bytes: 1,
  has_error: false,
  has_rage_click: false,
  has_dead_click: false,
  storage_prefix: '',
  created_at: new Date(BASE).toISOString(),
  updated_at: new Date(BASE).toISOString(),
} as ReplaySessionItem

// Profiling fixture for ?perfFixture=1: 19 one-second samples over the 19.5 s session. CPU is pinned at 95 % for
// samples 5-8 (a high_cpu breach) and FPS drops to 18 for samples 12-14 (a low_fps breach on a 60 Hz display).
function perfFixture(downsampled = false): SessionPerfResponse {
  return {
    interval_ms: 1000,
    device: { ram_mb: 4096, cores: 8, refresh_hz: 60 },
    metrics: ['cpu', 'mem_mb', 'fps'],
    downsampled,
    samples: Array.from({ length: 19 }, (_, i) => ({
      t: BASE + i * 1000,
      cpu: i >= 5 && i <= 8 ? 95 : 12,
      mem_mb: 200 + i * 2,
      fps: i >= 12 && i <= 14 ? 18 : 58,
    })),
  }
}

// The dashboard's real page chrome (sticky topbar + padded content) around the replay container, using the real
// useFillHeight hook, so e2e can assert the PAGE itself never overflows the viewport.
function ShellWrap({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement | null>(null)
  const height = useFillHeight(ref, true)
  return (
    <>
      <div className="console-topbar" />
      <main className="console-content">
        <div className="dash-replay-container" ref={ref} style={height ? { height } : undefined}>
          <div className="dash-replay-grid" style={{ gridTemplateColumns: 'minmax(0, 1fr)' }}>
            <div className="or-player-col">{children}</div>
          </div>
        </div>
      </main>
    </>
  )
}

const playerChunks: ReplayChunkDetail[] = Array.from({ length: FRAMES / PER_CHUNK }, (_, k) => ({
  chunk_seq: k,
  object_key: `chunk_${k}.gz`,
  size_bytes: 1,
  event_count: PER_CHUNK,
  created_at: new Date(BASE + (k + 1) * PER_CHUNK * SPACING + 500).toISOString(),
  url: `${location.origin}/__chunks/${k}.gz`,
}))

async function main() {
  const params = new URLSearchParams(location.search)
  const mode = params.get('mode') ?? 'scan'
  const delays: Record<number, number> = {}
  const delay = params.get('delay')
  if (delay) {
    const [seq, ms] = delay.split(':').map(Number)
    delays[seq] = ms
  }

  const { chunks, jpegs } = await buildChunks()
  const root = createRoot(document.getElementById('root')!)
  let session: ScreenReplaySession | null = null

  const expected = new Map<number, RGB>()
  const harness = {
    ready: false,
    base: BASE,
    frameTs: Array.from({ length: FRAMES }, (_, i) => BASE + i * SPACING),
    async expectedColor(i: number): Promise<RGB> {
      const cached = expected.get(i)
      if (cached) return cached
      const bin = atob(jpegs[i])
      const bytes = Uint8Array.from(bin, (ch) => ch.charCodeAt(0))
      const bmp = await createImageBitmap(new Blob([bytes], { type: 'image/jpeg' }), decodeOptions)
      const c = document.createElement('canvas')
      c.width = c.height = 16
      const ctx = c.getContext('2d')!
      ctx.drawImage(bmp, 0, 0, 16, 16)
      const d = ctx.getImageData(8, 8, 1, 1).data
      const rgb: RGB = [d[0], d[1], d[2]]
      expected.set(i, rgb)
      return rgb
    },
    setTs: (ts: number) => setPlayhead(ts),
    pixel(): RGB {
      const canvas = document.querySelector<HTMLCanvasElement>('[data-testid=screen-replay-canvas]')
      const ctx = canvas?.getContext('2d')
      if (!canvas || !ctx) return [0, 0, 0]
      const d = ctx.getImageData(Math.floor(canvas.width / 2), Math.floor(canvas.height / 2), 1, 1).data
      return [d[0], d[1], d[2]]
    },
    buffering: () => document.querySelector('.sr-badge') !== null,
    chunkPayloads: () => chunks,
    mountPlayer(opts?: { fillViewport?: boolean; shell?: boolean; perf?: SessionPerfResponse }) {
      const perf = opts?.perf ?? (params.get('perfFixture') === '1' ? perfFixture(params.get('perfDownsampled') === '1') : null)
      const player = (
        <ReplayPlayer
          session={sessionItem}
          chunks={playerChunks}
          timelineEvents={[]}
          projectPlatform="android"
          perf={perf}
        />
      )
      if (opts?.shell) {
        root.render(<ShellWrap>{player}</ShellWrap>)
        return
      }
      if (!opts?.fillViewport) {
        root.render(player)
        return
      }
      // Same nesting as the dashboard's replay page: container (fixed height) > grid > player column
      root.render(
        <div className="dash-replay-container" style={{ height: '100dvh' }}>
          <div className="dash-replay-grid" style={{ gridTemplateColumns: 'minmax(0, 1fr)' }}>
            <div className="or-player-col">{player}</div>
          </div>
        </div>,
      )
    },
    async settle() {
      await session?.whenIdle()
      await new Promise((r) => setTimeout(r, 400))
    },
    memoryMb(): number {
      ;(window as unknown as { gc?: () => void }).gc?.()
      return (performance as unknown as { memory: { usedJSHeapSize: number } }).memory.usedJSHeapSize / 1_048_576
    },
  }
  ;(window as unknown as { __harness: typeof harness }).__harness = harness

  if (mode === 'scan' || mode === 'lazy') {
    session = new ScreenReplaySession({
      seqs: chunks.map((_, k) => k),
      sessionStartMs: BASE,
      chunkCreatedAtMs: new Map(chunks.map((_, k) => [k, BASE + (k + 1) * PER_CHUNK * SPACING + 500])),
      fetchChunk: async (seq) => {
        if (delays[seq]) await new Promise((r) => setTimeout(r, delays[seq]))
        return chunks[seq]
      },
    })
    root.render(<RendererApp session={session} />)
    if (mode === 'scan') await session.start()
  } else if (mode === 'perf') {
    session = await perfSession()
    root.render(<RendererApp session={session} />)
    await session.start()
  }
  harness.ready = true
}

void main()
