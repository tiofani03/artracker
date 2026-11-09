import type { eventWithTime } from 'rrweb'

/** Native view hierarchy node as captured by the iOS / Android SDKs. */
export type ViewNode = {
  cls?: string
  frame?: [number, number, number, number] | { x: number; y: number; width: number; height: number }
  hidden?: boolean
  masked?: boolean
  text?: string
  children?: ViewNode[]
  [key: string]: unknown
}

/** One mobile screen snapshot. `ts` is epoch milliseconds. */
export type MobileFrame = {
  ts: number
  screen: string
  viewTree: ViewNode
  presentation?: string
  maskedCount?: number
}

/** One touch / tap. `ts` is epoch milliseconds; x/y are in root-frame points. */
export type TouchPoint = { ts: number; x: number; y: number; name?: string }

export type ReplayModel =
  | { kind: 'web'; events: eventWithTime[]; startMs: number; endMs: number }
  | {
      kind: 'mobile'
      platform: 'ios' | 'android'
      frames: MobileFrame[]
      touches: TouchPoint[]
      startMs: number
      endMs: number
    }
  | { kind: 'none'; reason: 'no-chunks' | 'no-visual-data' | 'load-error'; detail?: string }
