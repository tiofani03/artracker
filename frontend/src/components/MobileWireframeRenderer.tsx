import React, { useMemo } from 'react'
import type { MobileFrame, TouchPoint, ViewNode } from '../replay/model'
import { activeFrameAt, activeTouchAt } from '../replay/playback'
import { displayScreenName, fitScale, hasCapturedContent, nodeKinds, type NodeKind } from '../replay/wireframe'

export type { ViewNode }

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
  frames: MobileFrame[]
  touches: TouchPoint[]
  /** Absolute playhead position, epoch milliseconds. */
  currentTs: number
  platform: 'ios' | 'android'
  containerWidth?: number
  /** Height available for the device canvas; the screen is scaled to fit both axes. */
  maxHeight?: number
}

export const MobileWireframeRenderer: React.FC<MobileWireframeRendererProps> = ({
  frames,
  touches,
  currentTs,
  platform,
  containerWidth = 308,
  maxHeight = 492,
}) => {
  const activeFrame = useMemo(() => activeFrameAt(frames, currentTs), [frames, currentTs])
  const activeTouch = useMemo(() => activeTouchAt(touches, currentTs), [touches, currentTs])

  const screenName = activeFrame?.screen ?? 'UnknownScreen'
  const viewTree = activeFrame?.viewTree ?? null
  const presentation = activeFrame?.presentation ?? 'push'
  const maskedCount = activeFrame?.maskedCount ?? 0

  // Compute root frame and scaling factor
  const rootFrame = useMemo<ParsedFrame>(() => {
    if (viewTree) {
      const f = parseFrame(viewTree.frame)
      if (f && f[2] > 100 && f[3] > 100) return f
    }
    return platform === 'android' ? [0, 0, 412, 915] : [0, 0, 402, 874]
  }, [viewTree, platform])

  const [, , rootW, rootH] = rootFrame
  const scale = fitScale(rootFrame, { width: containerWidth, height: maxHeight })
  const scaledWidth = Math.round(rootW * scale)
  const scaledHeight = Math.round(rootH * scale)
  const contentCaptured = viewTree ? hasCapturedContent(viewTree) : true

  // Recursive renderer for native view tree nodes
  const renderNode = (
    node: ViewNode,
    key: string,
    depth = 0,
    inherited: ReadonlySet<NodeKind> = new Set(),
  ): React.ReactNode => {
    if (node.hidden) return null
    const frame = parseFrame(node.frame)
    if (!frame) return null
    const [x, y, w, h] = frame

    // Filter out degenerate 0-size nodes (unless it has children)
    if (w <= 0 && h <= 0 && (!node.children || node.children.length === 0)) {
      return null
    }

    const cls = node.cls || 'UIView'
    const kinds = nodeKinds(cls, inherited)
    const isText = kinds.has('text')
    const isButton = kinds.has('button')
    const isImage = kinds.has('image')
    const isSeparator = kinds.has('separator') || (h <= 2 && w > 20)
    const childInherited = new Set<NodeKind>(inherited)
    for (const k of kinds) childInherited.add(k)

    const classNames = [
      'mw-node',
      `mw-depth-${Math.min(depth, 5)}`,
      ...[...kinds].filter((k) => k !== 'separator').map((k) => `mw-node--${k}`),
      isSeparator && 'mw-node--separator',
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
          node.children.map((child, idx) => renderNode(child, `${key}-${idx}`, depth + 1, childInherited))}
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
            {displayScreenName(screenName)}
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
          height: `${scaledHeight}px`,
        }}
      >
        <div
          className="mw-canvas-scalable"
          style={{
            width: `${rootW}px`,
            height: `${rootH}px`,
            transform: `scale(${scale})`,
            transformOrigin: 'top left',
            marginLeft: `${Math.max(0, Math.round((containerWidth - scaledWidth) / 2))}px`,
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
        {!contentCaptured && (
          <div className="mw-capture-note" role="note">
            Only the native container views were captured for this screen. SwiftUI content inside
            _UIHostingView is not part of the UIKit view hierarchy the SDK records.
          </div>
        )}
      </div>
    </div>
  )
}
