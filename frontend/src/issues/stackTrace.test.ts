import { describe, expect, it } from 'vitest'
import { groupFrames, parseStackTrace } from './stackTrace'

const ANDROID = [
  'java.lang.NullPointerException: Attempt to invoke virtual method on a null object reference',
  '\tat com.gli.shop.checkout.CheckoutActivity.onPaymentResult(CheckoutActivity.kt:214)',
  '\tat androidx.lifecycle.LiveData.considerNotify(LiveData.java:133)',
  '\tat android.os.Handler.dispatchMessage(Handler.java:106)',
  'Caused by: java.lang.IllegalStateException: no order',
  '\tat com.gli.shop.data.OrderRepository.current(OrderRepository.kt:41)',
  '\t... 12 more',
].join('\n')

describe('parseStackTrace', () => {
  it('splits the trace into the thrown exception and its causes', () => {
    const sections = parseStackTrace(ANDROID)
    expect(sections.map((s) => s.title)).toEqual([
      'java.lang.NullPointerException: Attempt to invoke virtual method on a null object reference',
      'java.lang.IllegalStateException: no order',
    ])
    expect(sections[0].causedBy).toBe(false)
    expect(sections[1].causedBy).toBe(true)
  })

  it('marks app frames apart from framework frames', () => {
    const [main, cause] = parseStackTrace(ANDROID)
    expect(main.frames.map((f) => f.app)).toEqual([true, false, false])
    expect(cause.frames.map((f) => f.app)).toEqual([true, false])
  })

  it('splits a JVM frame into symbol and file location', () => {
    const [main] = parseStackTrace(ANDROID)
    expect(main.frames[0].symbol).toBe('com.gli.shop.checkout.CheckoutActivity.onPaymentResult')
    expect(main.frames[0].location).toBe('CheckoutActivity.kt:214')
  })

  it('splits a JS frame into symbol and location', () => {
    const [main] = parseStackTrace('TypeError: boom\n    at handleSubmit (src/PaymentForm.tsx:142:19)')
    expect(main.frames[0]).toMatchObject({
      symbol: 'handleSubmit',
      location: 'src/PaymentForm.tsx:142:19',
      app: true,
    })
  })

  it('treats node_modules frames as library frames', () => {
    const [main] = parseStackTrace('Error: x\n    at render (node_modules/react-dom/cjs/react-dom.js:10:5)')
    expect(main.frames[0].app).toBe(false)
  })

  it('returns no sections for an empty trace', () => {
    expect(parseStackTrace('')).toEqual([])
  })
})

describe('groupFrames', () => {
  it('collapses consecutive library frames into one group', () => {
    const [main] = parseStackTrace(ANDROID)
    const groups = groupFrames(main.frames)
    expect(groups.map((g) => (g.kind === 'app' ? 'app' : `lib×${g.frames.length}`))).toEqual([
      'app',
      'lib×2',
    ])
  })
})
