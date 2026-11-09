import type { Platform, ReplaySessionItem, TelemetryEvent } from '../api'

export type DetectedPlatform = 'web' | 'ios' | 'android'

export type PlatformDetectionResult = {
  platform: DetectedPlatform
  source: 'event' | 'session' | 'project' | 'default'
  label: string
  modelName: string
  resolution: string
}

export function detectSessionPlatform(
  session?: ReplaySessionItem | null,
  timelineEvents?: TelemetryEvent[] | null,
  projectPlatform?: Platform | null,
): PlatformDetectionResult {
  // 1. Check timeline events for explicit attributes, sdkMeta, or UA
  if (timelineEvents && timelineEvents.length > 0) {
    for (const ev of timelineEvents) {
      if (ev.attributes && typeof ev.attributes === 'object') {
        const attrs = ev.attributes as Record<string, unknown>
        const pStr = String(attrs.platform || attrs.os || attrs.device || '').toLowerCase()
        const ua = String(attrs.user_agent || attrs.ua || '').toLowerCase()
        const sdk = String(attrs.sdk || attrs.sdk_name || '').toLowerCase()

        if (
          pStr.includes('ios') ||
          pStr.includes('iphone') ||
          pStr.includes('ipad') ||
          ua.includes('iphone') ||
          ua.includes('ipad') ||
          sdk.includes('ios')
        ) {
          return {
            platform: 'ios',
            source: 'event',
            label: 'iOS',
            modelName: String(attrs.device || 'Apple iPhone 16 Pro'),
            resolution: '393 × 852',
          }
        }

        if (
          pStr.includes('android') ||
          pStr.includes('pixel') ||
          pStr.includes('samsung') ||
          ua.includes('android') ||
          sdk.includes('android')
        ) {
          return {
            platform: 'android',
            source: 'event',
            label: 'Android',
            modelName: String(attrs.device || 'Google Pixel 9'),
            resolution: '412 × 915',
          }
        }

        if (
          pStr.includes('web') ||
          ua.includes('mozilla') ||
          ua.includes('chrome') ||
          ua.includes('safari') ||
          sdk.includes('web')
        ) {
          return {
            platform: 'web',
            source: 'event',
            label: 'Web',
            modelName: String(attrs.browser || 'Desktop Browser'),
            resolution: '1440 × 900',
          }
        }
      }

      // Check event name / message (e.g. native mobile signals)
      const combined = `${ev.name || ''} ${ev.message || ''}`.toLowerCase()
      if (combined.includes('uikit') || combined.includes('swiftui') || (combined.includes('ios') && !combined.includes('anr'))) {
        return {
          platform: 'ios',
          source: 'event',
          label: 'iOS',
          modelName: 'Apple iPhone 16 Pro',
          resolution: '393 × 852',
        }
      }
      if (combined.includes('anr') || combined.includes('android') || combined.includes('kotlin') || combined.includes('jetpack')) {
        return {
          platform: 'android',
          source: 'event',
          label: 'Android',
          modelName: 'Google Pixel 9',
          resolution: '412 × 915',
        }
      }
    }
  }

  // 2. Check session ID, user_id or environment annotations
  if (session) {
    const sId = (session.id || '').toLowerCase()
    const uId = (session.user_id || '').toLowerCase()
    if (sId.includes('ios') || uId.includes('ios') || uId.includes('iphone') || uId.includes('apple')) {
      return {
        platform: 'ios',
        source: 'session',
        label: 'iOS',
        modelName: 'Apple iPhone 16 Pro',
        resolution: '393 × 852',
      }
    }
    if (sId.includes('android') || uId.includes('android') || uId.includes('pixel') || uId.includes('droid')) {
      return {
        platform: 'android',
        source: 'session',
        label: 'Android',
        modelName: 'Google Pixel 9',
        resolution: '412 × 915',
      }
    }
  }

  // 3. Fallback to project platform configuration
  if (projectPlatform === 'ios') {
    return {
      platform: 'ios',
      source: 'project',
      label: 'iOS',
      modelName: 'Apple iPhone 16 Pro',
      resolution: '393 × 852',
    }
  }
  if (projectPlatform === 'android') {
    return {
      platform: 'android',
      source: 'project',
      label: 'Android',
      modelName: 'Google Pixel 9',
      resolution: '412 × 915',
    }
  }
  if (projectPlatform === 'web') {
    return {
      platform: 'web',
      source: 'project',
      label: 'Web',
      modelName: 'Desktop Browser',
      resolution: '1440 × 900',
    }
  }

  // 4. Default fallback
  return {
    platform: 'web',
    source: 'default',
    label: 'Web',
    modelName: 'Desktop Browser',
    resolution: '1440 × 900',
  }
}
