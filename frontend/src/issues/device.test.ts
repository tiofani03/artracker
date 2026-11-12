import { describe, expect, it } from 'vitest'
import { eventContext } from './device'

describe('eventContext', () => {
  it('combines Android OS name/version and manufacturer/model', () => {
    expect(
      eventContext({ os: 'Android', os_version: '14', manufacturer: 'samsung', model: 'SM-A546E' }, '2.5.1'),
    ).toEqual({ device: 'samsung SM-A546E', os: 'Android 14', app: '2.5.1' })
  })

  it('does not repeat a version already inside the OS name', () => {
    expect(eventContext({ os: 'Android 14', os_version: '14' }, '').os).toBe('Android 14')
  })

  it('falls back to app_version from the device context and Unknown labels', () => {
    expect(eventContext({ app_version: '2.4.0' }, '')).toEqual({
      device: 'Unknown',
      os: 'Unknown',
      app: '2.4.0',
    })
  })
})
