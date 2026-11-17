import { describe, expect, it } from 'vitest'
import { contextRows, deviceSummary, peopleLink, sessionReplayLink, traitEntries } from './format'

describe('contextRows', () => {
  it('maps the Android session_start keys in a fixed order', () => {
    const rows = contextRows({
      platform: 'android',
      os_version: '14',
      model: 'Pixel 8',
      manufacturer: 'Google',
      app_version: '2.3.1',
      app_build: '231',
      locale: 'id-ID',
      tz: 'Asia/Jakarta',
      network: 'wifi',
      sdk_version: '1.0.3',
    })
    expect(rows.map((r) => r.label)).toEqual(['Device', 'OS', 'App', 'Locale', 'Timezone', 'Network', 'SDK'])
    expect(rows[0].value).toBe('Google Pixel 8')
    expect(rows[1].value).toBe('Android 14')
    expect(rows[2].value).toBe('2.3.1 (231)')
  })

  it('formats iOS and older iOS payload keys', () => {
    const rows = contextRows({ platform: 'ios', os_version: '17.4.1', model: 'iPhone15,2' })
    expect(rows.find((r) => r.label === 'OS')?.value).toBe('iOS 17.4.1')
    const legacy = contextRows({ os_name: 'iOS', os_version: '17.4', device_model: 'iPhone14,5', timezone: 'Asia/Jakarta' })
    expect(legacy.find((r) => r.label === 'Device')?.value).toBe('iPhone14,5')
    expect(legacy.find((r) => r.label === 'OS')?.value).toBe('iOS 17.4')
    expect(legacy.find((r) => r.label === 'Timezone')?.value).toBe('Asia/Jakarta')
  })

  it('omits missing keys and appends unknown keys sorted, skipping internal ones', () => {
    const rows = contextRows({ model: 'X', zeta: 1, alpha: true, session_id: 's', launch_type: 'cold', screen: { w: 1 } })
    expect(rows.map((r) => r.label)).toEqual(['Device', 'alpha', 'screen', 'zeta'])
    expect(rows.find((r) => r.label === 'screen')?.value).toBe('{"w":1}')
    expect(rows.find((r) => r.label === 'alpha')?.value).toBe('true')
  })

  it('shows emulator flag only when true', () => {
    expect(contextRows({ is_emulator: false })).toEqual([])
    expect(contextRows({ is_emulator: true })).toEqual([{ label: 'Emulator', value: 'Yes' }])
  })
})

describe('deviceSummary', () => {
  it('joins model, OS and app version', () => {
    expect(deviceSummary({ platform: 'android', model: 'Pixel 8', os_version: '14', app_version: '2.3.1', app_build: '231' })).toBe(
      'Pixel 8 · Android 14 · v2.3.1 (231)',
    )
  })
  it('falls back for an empty context', () => {
    expect(deviceSummary({})).toBe('Unknown device')
  })
})

describe('traitEntries', () => {
  it('sorts keys, stringifies values and respects max', () => {
    expect(traitEntries({ plan: 'pro', age: 30, beta: true })).toEqual([
      ['age', '30'],
      ['beta', 'true'],
      ['plan', 'pro'],
    ])
    expect(traitEntries({ b: 1, a: 2, c: 3 }, 2)).toEqual([
      ['a', '2'],
      ['b', '1'],
    ])
  })
})

describe('links', () => {
  it('encodes user and session ids', () => {
    expect(peopleLink('a/b c@x.id')).toBe('/dashboard/people?user=a%2Fb%20c%40x.id')
    expect(sessionReplayLink('ses_1 2')).toBe('/dashboard/replays?session=ses_1%202')
  })
})
