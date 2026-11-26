import type { eventWithTime } from 'rrweb'
import type { ReplaySessionItem, SessionPerfResponse, TelemetryEvent } from '../api'
import type { MobileFrame, TouchPoint } from './model'

export type ExportFormat = 'video' | 'images-zip' | 'offline-html' | 'raw-bundle'

export type ExportRangeMode = 'full' | 'clip'

export type ExportOptions = {
  format: ExportFormat
  rangeMode: ExportRangeMode
  clipStartMs: number // offset ms relative to session start (0..durationMs)
  clipEndMs: number   // offset ms relative to session start (0..durationMs)
  // Format-specific options
  videoFps?: number
  burnTouchOverlays?: boolean
  wireframeTheme?: 'light' | 'dark'
  imagesKeyframeMode?: 'all-frames' | 'event-driven'
}

export type ExportProgress = {
  phase: 'idle' | 'fetching' | 'processing' | 'encoding' | 'packaging' | 'done' | 'error'
  percent: number
  message: string
  currentFrame?: number
  totalFrames?: number
}

export type SlicedSessionData = {
  session: ReplaySessionItem
  startAbsMs: number
  endAbsMs: number
  clipStartOffsetMs: number
  clipEndOffsetMs: number
  timelineEvents: TelemetryEvent[]
  perf: SessionPerfResponse | null
  // Mode-specific visual data
  screenFrames?: Array<{
    ts: number
    screen: string
    w: number
    h: number
    vw: number
    vh: number
    mime: 'image/jpeg'
    chunkSeq: number
    index: number
    masked: number
  }>
  screenTouches?: Array<{ ts: number; x: number; y: number; kind: 'down' | 'move' | 'up' }>
  mobileFrames?: MobileFrame[]
  mobileTouches?: TouchPoint[]
  webEvents?: eventWithTime[]
}
