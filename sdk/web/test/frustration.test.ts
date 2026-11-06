import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { FrustrationEngine, type DeadClickSignal, type RageClickSignal } from '../src/frustration.ts'
import { gunzipToString, gzipString } from '../src/gzip.ts'
import { startReplayRecorder } from '../src/recorder.ts'

describe('FrustrationEngine - Rage Click & Dead Click', () => {
  it('flags rage click when 3+ clicks hit the same element within 500ms', () => {
    const rageSignals: RageClickSignal[] = []
    let now = 10_000

    const engine = new FrustrationEngine({
      now: () => now,
      onRageClick: (sig) => rageSignals.push(sig),
    })

    engine.recordClick({ x: 120, y: 240, selector: 'button#pay-submit', isInteractive: true })
    now += 150
    engine.recordClick({ x: 122, y: 241, selector: 'button#pay-submit', isInteractive: true })
    assert.equal(rageSignals.length, 0)

    now += 180 // 330ms total (< 500ms)
    engine.recordClick({ x: 119, y: 239, selector: 'button#pay-submit', isInteractive: true })

    assert.equal(rageSignals.length, 1)
    assert.equal(rageSignals[0].count, 3)
    assert.equal(rageSignals[0].selector, 'button#pay-submit')
    assert.equal(engine.peekFlags().hasRageClick, true)

    const consumed = engine.consumeChunkFlags()
    assert.equal(consumed.hasRageClick, true)
    assert.equal(engine.peekFlags().hasRageClick, false)
    engine.dispose()
  })

  it('does NOT flag rage click when clicks are spaced > 500ms apart', () => {
    const rageSignals: RageClickSignal[] = []
    let now = 20_000

    const engine = new FrustrationEngine({
      now: () => now,
      onRageClick: (sig) => rageSignals.push(sig),
    })

    engine.recordClick({ x: 50, y: 50, selector: 'button#save', isInteractive: true })
    now += 300
    engine.recordClick({ x: 50, y: 50, selector: 'button#save', isInteractive: true })
    now += 300 // 600ms since 1st click, so only 2 clicks inside 500ms window
    engine.recordClick({ x: 50, y: 50, selector: 'button#save', isInteractive: true })

    assert.equal(rageSignals.length, 0)
    assert.equal(engine.peekFlags().hasRageClick, false)
    engine.dispose()
  })

  it('flags dead click when interactive button has no DOM mutation or network call in 1500ms', () => {
    const deadSignals: DeadClickSignal[] = []
    const scheduled = new Map<number, { fn: () => void; ms: number }>()
    let timerId = 1

    const engine = new FrustrationEngine({
      now: () => 5000,
      setTimer: (fn, ms) => {
        const id = timerId++
        scheduled.set(id, { fn, ms })
        return id
      },
      clearTimer: (id) => {
        scheduled.delete(id as number)
      },
      onDeadClick: (sig) => deadSignals.push(sig),
    })

    engine.recordClick({ x: 80, y: 90, selector: 'button.apply-coupon', isInteractive: true })
    assert.equal(scheduled.size, 1)
    assert.equal(deadSignals.length, 0)

    // Simulate 1500ms elapsing without notifyActivity()
    for (const entry of [...scheduled.values()]) {
      assert.equal(entry.ms, 1500)
      entry.fn()
    }

    assert.equal(deadSignals.length, 1)
    assert.equal(deadSignals[0].selector, 'button.apply-coupon')
    assert.equal(engine.consumeChunkFlags().hasDeadClick, true)
  })

  it('cancels dead click when DOM mutation or network call occurs within 1500ms', () => {
    const deadSignals: DeadClickSignal[] = []
    const scheduled = new Map<number, () => void>()
    let timerId = 1

    const engine = new FrustrationEngine({
      setTimer: (fn) => {
        const id = timerId++
        scheduled.set(id, fn)
        return id
      },
      clearTimer: (id) => {
        scheduled.delete(id as number)
      },
      onDeadClick: (sig) => deadSignals.push(sig),
    })

    engine.recordClick({ x: 80, y: 90, selector: 'button#submit', isInteractive: true })
    assert.equal(scheduled.size, 1)

    // Simulate fetch() or DOM Mutation firing at 200ms
    engine.notifyActivity()
    assert.equal(scheduled.size, 0)
    assert.equal(deadSignals.length, 0)
    assert.equal(engine.peekFlags().hasDeadClick, false)
  })
})

describe('GZIP chunk compression & ReplayRecorder upload pipeline', () => {
  it('compresses rrweb JSON into valid gzip stream and decompresses identically', async () => {
    const sample = JSON.stringify([
      { type: 4, data: { href: 'https://app.example.com', width: 1280, height: 720 }, timestamp: 1000 },
      { type: 2, data: { node: { type: 0, childNodes: [] } }, timestamp: 1005 },
    ])
    const gz = await gzipString(sample)
    assert.equal(gz[0], 0x1f)
    assert.equal(gz[1], 0x8b)
    const restored = await gunzipToString(gz)
    assert.equal(restored, sample)
  })

  it('uploads gzipped chunk to presigned URL and sends frustration/error flags in chunk-ack', async () => {
    const calls: Array<{ url: string; method: string; body?: unknown }> = []
    let uploadedBytes: Uint8Array | null = null

    const mockFetch: typeof fetch = async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.endsWith('/replay/chunk-url')) {
        calls.push({ url, method, body: JSON.parse(String(init?.body)) })
        return new Response(
          JSON.stringify({
            upload_url: 'https://r2.mock.local/projects/1/sessions/sess_abc12345/chunk_0.gz',
            object_key: 'projects/1/sessions/sess_abc12345/chunk_0.gz',
            expires_in: 300,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      if (url.startsWith('https://r2.mock.local/')) {
        uploadedBytes = init?.body as unknown as Uint8Array
        calls.push({ url, method })
        return new Response(null, { status: 200 })
      }
      if (url.endsWith('/replay/chunk-ack')) {
        calls.push({ url, method, body: JSON.parse(String(init?.body)) })
        return new Response(
          JSON.stringify({ status: 'acknowledged', session_id: 'sess_abc12345', chunk_seq: 0, chunk_count: 1 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response('{}', { status: 200 })
    }

    const recorder = startReplayRecorder({
      endpoint: 'https://art.example.com/api/v1',
      apiKey: 'art_live_testkey',
      sessionId: 'sess_abc12345',
      userId: 'nadia@fintech.id',
      fetchImpl: mockFetch,
      recordImpl: ({ emit }) => {
        emit({ type: 4, data: { href: 'https://shop.id', width: 1200, height: 800 }, timestamp: 1000 } as never)
        emit({ type: 2, data: {}, timestamp: 2500 } as never)
        return () => undefined
      },
    })

    recorder.markError('Payment gateway 502')
    await recorder.stop()

    assert.equal(calls.length, 3)
    assert.equal(calls[0].method, 'POST')
    assert.equal(calls[1].method, 'PUT')
    assert.ok(uploadedBytes && uploadedBytes[0] === 0x1f && uploadedBytes[1] === 0x8b)

    const ackBody = calls[2].body as Record<string, unknown>
    assert.equal(ackBody.session_id, 'sess_abc12345')
    assert.equal(ackBody.chunk_seq, 0)
    assert.equal(ackBody.event_count, 2)
    assert.equal(ackBody.duration_ms, 1500)
    assert.equal(ackBody.has_error, true)
  })
})
