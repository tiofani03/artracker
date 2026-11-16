import { useLayoutEffect, useState, type RefObject } from 'react'
import { fillHeight } from './fit'

const px = (v: string) => Number.parseFloat(v) || 0

const SHORT_VIEWPORT_PX = 500
const SHORT_VIEWPORT_MIN_PX = 560

/** Space the element's ancestors keep below it (padding, border, margin): a viewport-high child must leave it free. */
export function chromeBelow(el: HTMLElement): number {
  let total = 0
  for (let n = el.parentElement; n; n = n.parentElement) {
    const cs = getComputedStyle(n)
    total += px(cs.paddingBottom) + px(cs.borderBottomWidth) + px(cs.marginBottom)
  }
  return total
}

/**
 * Pixel height that makes `ref`'s element end exactly at the bottom of the viewport, so the page itself never
 * scrolls. Re-measured on window resize and whenever the page body changes size (e.g. a banner appearing above).
 * null until measured; while `active` is false nothing is measured.
 */
export function useFillHeight(ref: RefObject<HTMLElement | null>, active: boolean, min = 320): number | null {
  const [height, setHeight] = useState<number | null>(null)

  useLayoutEffect(() => {
    if (!active) return
    const measure = () => {
      const el = ref.current
      if (!el) return
      const top = el.getBoundingClientRect().top + window.scrollY
      // Short viewports (a phone on its side): a viewport-high workspace would leave the stage a
      // sliver, so let the page scroll and give the workspace a usable height instead.
      const floor = window.innerHeight < SHORT_VIEWPORT_PX ? Math.max(min, SHORT_VIEWPORT_MIN_PX) : min
      setHeight(fillHeight(window.innerHeight, top, chromeBelow(el), floor))
    }
    measure()
    window.addEventListener('resize', measure)
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure)
    ro?.observe(document.body)
    return () => {
      window.removeEventListener('resize', measure)
      ro?.disconnect()
    }
  }, [active, ref, min])

  return height
}
