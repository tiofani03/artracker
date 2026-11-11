const MIN_SCALE = 0.25

/** Scale (≤ 1, ≥ 0.25) that makes `content` fit inside `avail`. Unmeasured (zero-sized) boxes give 1. */
export function fitScale(avail: { w: number; h: number }, content: { w: number; h: number }): number {
  if (avail.w <= 0 || avail.h <= 0 || content.w <= 0 || content.h <= 0) return 1
  return Math.max(MIN_SCALE, Math.min(1, avail.w / content.w, avail.h / content.h))
}

/** Height for a panel that starts `top` px down the page and should end `bottomPad` px above the viewport edge. */
export function fillHeight(viewportH: number, top: number, bottomPad = 16, min = 480): number {
  return Math.max(min, Math.round(viewportH - top - bottomPad))
}
