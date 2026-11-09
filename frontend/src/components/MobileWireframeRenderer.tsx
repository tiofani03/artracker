import React, { useMemo } from 'react'
import type { TelemetryEvent } from '../api'

export type ViewNode = {
  cls?: string
  frame?: [number, number, number, number] | { x: number; y: number; width: number; height: number }
  hidden?: boolean
  masked?: boolean
  text?: string
  children?: ViewNode[]
  [key: string]: unknown
}

export type ParsedFrame = [number, number, number, number]

export function parseFrame(frame: unknown): ParsedFrame | null {
  if (Array.isArray(frame) && frame.length >= 4) {
    const x = Number(frame[0]) || 0
    const y = Number(frame[1]) || 0
    const w = Number(frame[2]) || 0
    const h = Number(frame[3]) || 0
    return [x, y, w, h]
  }
  if (frame && typeof frame === 'object') {
    const f = frame as Record<string, unknown>
    const x = Number(f.x ?? f.left ?? 0)
    const y = Number(f.y ?? f.top ?? 0)
    const w = Number(f.w ?? f.width ?? 0)
    const h = Number(f.h ?? f.height ?? 0)
    return [x, y, w, h]
  }
  return null
}

interface MobileWireframeRendererProps {
  screenEvents: TelemetryEvent[]
  currentTimeMs: number
  baseTimestampMs: number
  timelineEvents: TelemetryEvent[]
  platform: 'ios' | 'android' | 'web'
  containerWidth?: number
}

export const MobileWireframeRenderer: React.FC<MobileWireframeRendererProps> = ({
  screenEvents,
  currentTimeMs,
  baseTimestampMs,
  timelineEvents,
  platform,
  containerWidth = 308,
}) => {
  // Find current active screen based on scrubber playback timestamp
  const activeScreenEvent = useMemo(() => {
    if (screenEvents.length === 0) return null
    const currentTs = baseTimestampMs + currentTimeMs

    // Find the latest screen event that occurred at or before currentTimeMs
    const past = screenEvents.filter((ev) => {
      const ts = new Date(ev.event_timestamp).getTime()
      return !Number.isNaN(ts) && ts <= currentTs
    })

    if (past.length > 0) {
      return past[past.length - 1]
    }
    return screenEvents[0]
  }, [screenEvents, baseTimestampMs, currentTimeMs])

  const { screenName, viewTree, presentation, maskedCount } = useMemo(() => {
    if (!activeScreenEvent) {
      return { screenName: 'UnknownScreen', viewTree: null, presentation: 'push', maskedCount: 0 }
    }
    const attrs = (activeScreenEvent.attributes || {}) as Record<string, unknown>
    const name = String(attrs.screen || activeScreenEvent.name || 'MainScreen')
    const tree = (attrs.view_tree || attrs.tree || null) as ViewNode | null
    const pres = String(attrs.presentation || 'push')
    const masked = Number(attrs.masked_count || 0)
    return { screenName: name, viewTree: tree, presentation: pres, maskedCount: masked }
  }, [activeScreenEvent])

  // Active touch/gesture ripple within ±600ms of current scrubber time
  const activeTouch = useMemo(() => {
    const currentTs = baseTimestampMs + currentTimeMs
    for (const ev of timelineEvents) {
      const ts = new Date(ev.event_timestamp).getTime()
      if (Number.isNaN(ts) || Math.abs(ts - currentTs) > 600) continue

      const kind = (ev.kind || '').toLowerCase()
      const name = (ev.name || '').toLowerCase()
      const attrs = (ev.attributes || {}) as Record<string, unknown>

      if (
        name.includes('touch') ||
        name.includes('tap') ||
        name.includes('click') ||
        kind.includes('gesture') ||
        attrs.touch ||
        (attrs.x !== undefined && attrs.y !== undefined)
      ) {
        const touchObj = (attrs.touch as Record<string, unknown>) || attrs
        const x = Number(touchObj.x ?? 0)
        const y = Number(touchObj.y ?? 0)
        return { x, y, name: ev.name || 'tap' }
      }
    }
    return null
  }, [timelineEvents, baseTimestampMs, currentTimeMs])

  // Compute root frame and scaling factor
  const rootFrame = useMemo<ParsedFrame>(() => {
    if (viewTree) {
      const f = parseFrame(viewTree.frame)
      if (f && f[2] > 100 && f[3] > 100) return f
    }
    return platform === 'android' ? [0, 0, 412, 915] : [0, 0, 402, 874]
  }, [viewTree, platform])

  const [, , rootW, rootH] = rootFrame
  const scale = containerWidth > 0 && rootW > 0 ? containerWidth / rootW : 0.76
  const scaledHeight = Math.round(rootH * scale)

  // Recursive renderer for native view tree nodes
  const renderNode = (node: ViewNode, key: string, depth = 0): React.ReactNode => {
    if (node.hidden) return null
    const frame = parseFrame(node.frame)
    if (!frame) return null
    const [x, y, w, h] = frame

    // Filter out degenerate 0-size nodes (unless it has children)
    if (w <= 0 && h <= 0 && (!node.children || node.children.length === 0)) {
      return null
    }

    const cls = node.cls || 'UIView'
    const isAlertOrModal =
      cls.includes('Alert') || cls.includes('Modal') || cls.includes('Dialog') || cls.includes('ActionSheet')
    const isToolbar =
      cls.includes('Toolbar') || cls.includes('NavigationBar') || cls.includes('AppBar') || cls.includes('Header')
    const isTableOrCollection =
      cls.includes('TableView') || cls.includes('CollectionView') || cls.includes('RecyclerView') || cls.includes('ScrollView')
    const isCellOrRow =
      cls.includes('Cell') || cls.includes('Item') || cls.includes('Card') || cls.includes('Row')
    const isSectionHeader =
      cls.includes('SectionView') || cls.includes('HeaderFooter') || cls.includes('SectionHeader')
    const isText =
      cls.includes('Label') || cls.includes('TextView') || cls.includes('Text') || cls.includes('Title')
    const isButton =
      cls.includes('Button') || cls.includes('Action') || cls.includes('Clickable')
    const isSeparator =
      cls.includes('Separator') || (h <= 2 && w > 20)
    const isImage =
      cls.includes('ImageView') || cls.includes('Icon') || cls.includes('Image')

    // Determine custom class styling
    const classNames = [
      'mw-node',
      `mw-depth-${Math.min(depth, 5)}`,
      isAlertOrModal && 'mw-node--modal',
      isToolbar && 'mw-node--toolbar',
      isTableOrCollection && 'mw-node--scroll',
      isCellOrRow && 'mw-node--cell',
      isSectionHeader && 'mw-node--section',
      isText && 'mw-node--text',
      isButton && 'mw-node--button',
      isSeparator && 'mw-node--separator',
      isImage && 'mw-node--image',
    ]
      .filter(Boolean)
      .join(' ')

    const nodeStyle: React.CSSProperties = {
      position: 'absolute',
      left: `${x}px`,
      top: `${y}px`,
      width: `${w}px`,
      height: `${h}px`,
    }

    // Specific text extraction or skeleton rendering
    const textContent = typeof node.text === 'string' && node.text.trim() ? node.text.trim() : null

    return (
      <div
        key={key}
        className={classNames}
        style={nodeStyle}
        data-cls={cls}
        title={`${cls} [${Math.round(x)}, ${Math.round(y)}, ${Math.round(w)}, ${Math.round(h)}]`}
      >
        {textContent && <span className="mw-text-label">{textContent}</span>}

        {isText && !textContent && !node.children?.length && (
          <div
            className={`mw-skeleton-text ${h > 24 ? 'mw-skeleton-text--lg' : 'mw-skeleton-text--sm'}`}
            style={{ width: `${Math.min(w - 4, Math.max(28, w * 0.75))}px` }}
          />
        )}

        {isButton && !textContent && !node.children?.length && (
          <div className="mw-button-placeholder">
            <span className="mw-skeleton-btn" />
          </div>
        )}

        {isImage && !node.children?.length && (
          <div className="mw-image-placeholder">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.5">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
              <circle cx="8.5" cy="8.5" r="1.5" />
              <polyline points="21 15 16 10 5 21" />
            </svg>
          </div>
        )}

        {node.children &&
          node.children.map((child, idx) => renderNode(child, `${key}-${idx}`, depth + 1))}
      </div>
    )
  }

  return (
    <div className="mw-viewport-container" style={{ width: `${containerWidth}px` }}>
      {/* Dynamic Screen Header Banner */}
      <div className="mw-screen-banner">
        <div className="mw-screen-banner__left">
          <span className="mw-screen-dot" />
          <strong className="mw-screen-title" title={screenName}>
            {screenName}
          </strong>
        </div>
        <div className="mw-screen-banner__right">
          {presentation && presentation !== 'push' && (
            <span className="mw-pres-badge">{presentation.toUpperCase()}</span>
          )}
          {maskedCount > 0 && (
            <span className="mw-masked-badge" title={`${maskedCount} elements masked for privacy`}>
              🛡️ {maskedCount}
            </span>
          )}
        </div>
      </div>

      {/* Actual Scaled Native Viewport Canvas */}
      <div
        className="mw-canvas-stage"
        style={{
          width: `${containerWidth}px`,
          height: `${Math.min(460, scaledHeight)}px`,
        }}
      >
        <div
          className="mw-canvas-scalable"
          style={{
            width: `${rootW}px`,
            height: `${rootH}px`,
            transform: `scale(${scale})`,
            transformOrigin: 'top left',
          }}
        >
          {viewTree ? (
            renderNode(viewTree, 'root-0', 0)
          ) : (
            <div className="mw-empty-screen">
              <span>Awaiting view hierarchy snapshot…</span>
            </div>
          )}

          {/* Active Touch Gesture Ripple */}
          {activeTouch && (
            <div
              className="mw-touch-ripple"
              style={{
                left: `${activeTouch.x}px`,
                top: `${activeTouch.y}px`,
              }}
              aria-hidden="true"
            >
              <div className="mw-touch-ring" />
              <div className="mw-touch-core" />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
