const MIN_SCALE = 0.25

/**
 * Scale (≥ 0.25, ≤ `maxScale`) that makes `content` fit inside `avail`. Unmeasured (zero-sized) boxes give 1.
 * `maxScale` defaults to 1 (never enlarge); a device chassis passes a higher ceiling so it can fill a tall stage.
 */
export function fitScale(
  avail: { w: number; h: number },
  content: { w: number; h: number },
  maxScale = 1,
): number {
  if (avail.w <= 0 || avail.h <= 0 || content.w <= 0 || content.h <= 0) return 1
  return Math.max(MIN_SCALE, Math.min(maxScale, avail.w / content.w, avail.h / content.h))
}

/** Height for a panel that starts `top` px down the page and should end `bottomPad` px above the viewport edge. */
export function fillHeight(viewportH: number, top: number, bottomPad = 16, min = 480): number {
  return Math.max(min, Math.round(viewportH - top - bottomPad))
}
